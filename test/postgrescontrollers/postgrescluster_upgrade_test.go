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
	"context"
	"fmt"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pgtesthelpers "github.com/splunk/splunk-operator/test/postgrescontrollers/helpers"
	"github.com/splunk/splunk-operator/test/testenv"
)

// Major-version upgrade test configuration. The source/target pair must be an
// adjacent-major hop that the deployed CNPG version supports; the workflow
// rejects multi-major jumps. The snapshot class must name a VolumeSnapshotClass
// that exists in the cluster. The shared snapshot E2E setup installs one and
// exports the matching POSTGRES_E2E_VOLUME_SNAPSHOT_CLASS.
var (
	pgUpgradeSourceVersion = testenv.GetEnvWithDefault("TEST_PG_SOURCE_VERSION", "15")
	pgUpgradeTargetVersion = testenv.GetEnvWithDefault("TEST_PG_TARGET_VERSION", "16")
	pgVolumeSnapshotClass  = testenv.GetEnvWithDefault("POSTGRES_E2E_VOLUME_SNAPSHOT_CLASS", "sok-postgres-ebs-csi")
)

// seedTableRowCount is the number of rows written before the upgrade; the same
// count and checksum must be observable afterwards.
const seedTableRowCount = 50

var _ = Describe("postgrescontrollers, integration, postgres-upgrade", Label("tier:e2e-full", "cloud:aws", "feature:postgres"), func() {

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

	It("postgrescontrollers, integration, postgres-upgrade: can upgrade a PostgresCluster to the next supported major version",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.LongTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			snapshots, _ := pgtesthelpers.RequireVolumeSnapshotClass(ctx, pgVolumeSnapshotClass)
			pgtesthelpers.RegisterSnapshotFailureDump(kubeClient, snapshots, ns)

			pgClass := createPGClassWithSnapshotBackup(ctx, kubeClient, ns, pgUpgradeSourceVersion)
			pgCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "major-upgrade", Namespace: ns},
				Spec: platformv1alpha1.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
					PostgresVersion:       ptr.To(pgUpgradeSourceVersion),
					// allow is inert until spec.postgresVersion actually changes, so
					// setting it up front keeps the upgrade request below a single patch.
					PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{
						Allow:    ptr.To(true),
						Strategy: ptr.To(mvutypes.MajorUpgradeFlowPgUpgrade),
					},
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			clusterKey := types.NamespacedName{Name: pgCluster.Name, Namespace: ns}

			By("waiting for PostgresCluster to reach Ready on the source major version")
			readySource := pgtesthelpers.WaitForReadyPostgresCluster(ctx, kubeClient, clusterKey)
			Expect(readySource.Status.CurrentPgVersion).To(Equal(majorOf(pgUpgradeSourceVersion)))

			By("deploying a PostgresDatabase and waiting for Ready")
			pgDB := pgtesthelpers.CreateReadyPostgresDatabase(ctx, kubeClient, ns, "upgrade-db", pgCluster.Name, "appdb")
			dbKey := types.NamespacedName{Name: pgDB.Name, Namespace: ns}
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(presentRoleNames(pc)).To(ContainElements("appdb_admin", "appdb_rw"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			adminCreds, _ := managedRoleCredentials(ctx, kubeClient, dbKey, "appdb")

			By("creating database objects and inserting deterministic test data as the managed admin role")
			// Written through the published RW endpoint so the same statement path
			// exercises the connection contract the upgrade must preserve. Seeded as
			// the managed admin role (the database owner) rather than the superuser:
			// that is the path an application takes, and it is what gives the rw role
			// its inherited privileges to assert after the upgrade.
			runSQLAs(ctx, kubeClient, deployment, clusterKey, adminCreds, "appdb", fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS upgrade_fixture (id int PRIMARY KEY, payload text NOT NULL);
TRUNCATE upgrade_fixture;
INSERT INTO upgrade_fixture (id, payload)
  SELECT g, 'row-' || g FROM generate_series(1, %d) AS g;
CREATE INDEX IF NOT EXISTS upgrade_fixture_payload_idx ON upgrade_fixture (payload);
CREATE OR REPLACE VIEW upgrade_fixture_view AS SELECT id, payload FROM upgrade_fixture WHERE id %% 2 = 0;
`, seedTableRowCount))

			// Serialize the ordered rows directly. This is deterministic across
			// PostgreSQL majors and avoids depending on a hash implementation.
			const fixtureStateSQL = `SELECT count(*) || ':' || coalesce(string_agg(id::text || '=' || payload, ',' ORDER BY id), '') FROM upgrade_fixture`
			preUpgradeState := runSQLAs(ctx, kubeClient, deployment, clusterKey, adminCreds, "appdb", fixtureStateSQL)
			Expect(preUpgradeState).To(HavePrefix(fmt.Sprintf("%d:", seedTableRowCount)))
			AddReportEntry("pre-upgrade fixture state", preUpgradeState)

			By("requesting an upgrade to the target major version")
			Expect(kubeClient.Get(ctx, clusterKey, pgCluster)).To(Succeed())
			patch := client.MergeFrom(pgCluster.DeepCopy())
			pgCluster.Spec.PostgresVersion = ptr.To(pgUpgradeTargetVersion)
			Expect(kubeClient.Patch(ctx, pgCluster, patch)).To(Succeed())

			// Individual phases are transient and can be missed between polls, so
			// accumulate everything observed on the way to Completed and assert on
			// the durable evidence (recorded backup names, timestamps) afterwards.
			var observedPhases []string
			By("observing the upgrade advance through status to Completed")
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				entry := currentUpgradeEntry(g, pc)
				g.Expect(entry.Phase).NotTo(BeNil())
				if len(observedPhases) == 0 || observedPhases[len(observedPhases)-1] != *entry.Phase {
					observedPhases = append(observedPhases, *entry.Phase)
				}
				if *entry.Phase == string(mvutypes.Failed) {
					StopTrying(fmt.Sprintf("upgrade terminally failed: %s; backups: %s",
						upgradeConditionSummary(entry), upgradeBackupSummary(ctx, kubeClient, clusterKey))).Now()
				}
				g.Expect(*entry.Phase).To(Equal(string(mvutypes.Completed)),
					"upgrade did not complete: %s", upgradeConditionSummary(entry))
			}, testenv.MediumLongTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying both backup safety gates completed")
			pc := &platformv1alpha1.PostgresCluster{}
			Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
			entry := pc.Status.PostgresMajorUpgradeStatus[len(pc.Status.PostgresMajorUpgradeStatus)-1]
			Expect(entry.SourcePgVersion).To(HaveValue(Equal(majorOf(pgUpgradeSourceVersion))))
			Expect(entry.TargetPgVersion).To(HaveValue(Equal(pgUpgradeTargetVersion)))
			Expect(entry.BackupNames).NotTo(BeNil(), "expected upgrade to record its backup baselines")
			Expect(entry.BackupNames.PreUpgrade).NotTo(BeNil(), "expected a pre-upgrade backup baseline")
			Expect(entry.BackupNames.PostUpgrade).NotTo(BeNil(), "expected a post-upgrade backup baseline")

			// Both gates are only satisfied by Completed CNPG Backups.
			preUpgradeBackup := &cnpgv1.Backup{}
			Expect(kubeClient.Get(ctx, types.NamespacedName{Name: *entry.BackupNames.PreUpgrade, Namespace: ns}, preUpgradeBackup)).To(Succeed())
			Expect(preUpgradeBackup.Status.Phase).To(Equal(cnpgv1.BackupPhase(cnpgv1.BackupPhaseCompleted)))
			postUpgradeBackup := &cnpgv1.Backup{}
			Expect(kubeClient.Get(ctx, types.NamespacedName{Name: *entry.BackupNames.PostUpgrade, Namespace: ns}, postUpgradeBackup)).To(Succeed())
			Expect(postUpgradeBackup.Status.Phase).To(Equal(cnpgv1.BackupPhase(cnpgv1.BackupPhaseCompleted)))

			By("capturing upgrade duration and observed status transitions")
			Expect(entry.StartedAt).NotTo(BeNil())
			Expect(entry.CompletedAt).NotTo(BeNil())
			AddReportEntry("upgrade duration", entry.CompletedAt.Sub(entry.StartedAt.Time).String())
			AddReportEntry("observed upgrade phases", strings.Join(observedPhases, " -> "))
			AddReportEntry("upgrade conditions", upgradeConditionSummary(entry))

			By("waiting for PostgresCluster to return to Ready on the target major version")
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(pc.Status.Phase).NotTo(BeNil())
				g.Expect(*pc.Status.Phase).To(Equal("Ready"), "cluster did not return to Ready: %s", clusterConditionSummary(pc))
				g.Expect(pc.Status.CurrentPgVersion).To(Equal(majorOf(pgUpgradeTargetVersion)))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying PostgreSQL runs the requested target major version")
			Eventually(func(g Gomega) {
				cnpg := &cnpgv1.Cluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, cnpg)).To(Succeed())
				g.Expect(cnpg.Status.Phase).To(Equal(cnpgv1.PhaseHealthy))
				g.Expect(cnpg.Spec.ImageName).To(HaveSuffix(":" + pgUpgradeTargetVersion))
				g.Expect(cnpg.Status.PGDataImageInfo).NotTo(BeNil())
				g.Expect(fmt.Sprintf("%d", cnpg.Status.PGDataImageInfo.MajorVersion)).To(Equal(majorOf(pgUpgradeTargetVersion)))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			// Read back through the published RW endpoint: a successful query proves
			// the endpoint is still usable and the seeded data survived together.
			By("verifying the database is reachable and the seeded data is unchanged")
			Expect(runSQL(ctx, kubeClient, deployment, clusterKey, "appdb", "SHOW server_version")).To(HavePrefix(pgUpgradeTargetVersion))
			Expect(runSQLAs(ctx, kubeClient, deployment, clusterKey, adminCreds, "appdb", fixtureStateSQL)).To(Equal(preUpgradeState))
			Expect(runSQLAs(ctx, kubeClient, deployment, clusterKey, adminCreds, "appdb",
				"SELECT count(*) FROM upgrade_fixture_view")).To(Equal(fmt.Sprintf("%d", seedTableRowCount/2)))

			By("verifying managed roles remain available and PostgresDatabase is Ready")
			Eventually(func(g Gomega) {
				pd := &platformv1alpha1.PostgresDatabase{}
				g.Expect(kubeClient.Get(ctx, dbKey, pd)).To(Succeed())
				g.Expect(pd.Status.Phase).NotTo(BeNil())
				g.Expect(*pd.Status.Phase).To(Equal("Ready"))

				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				g.Expect(presentRoleNames(pc)).To(ContainElements("appdb_admin", "appdb_rw"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			// Roles must still be able to authenticate, not merely be listed in
			// status: re-read the published credentials and let PostgreSQL confirm
			// the identity it logged in as. The rw role additionally reads the
			// fixture, proving its inherited privileges survived the upgrade.
			By("verifying the managed roles can still log in with their published credentials")
			postAdminCreds, postRWCreds := managedRoleCredentials(ctx, kubeClient, dbKey, "appdb")
			Expect(runSQLAs(ctx, kubeClient, deployment, clusterKey, postAdminCreds, "appdb",
				"SELECT current_user")).To(Equal(postAdminCreds.user))
			Expect(runSQLAs(ctx, kubeClient, deployment, clusterKey, postRWCreds, "appdb",
				"SELECT current_user")).To(Equal(postRWCreds.user))
			Expect(runSQLAs(ctx, kubeClient, deployment, clusterKey, postRWCreds, "appdb",
				"SELECT count(*) FROM upgrade_fixture")).To(Equal(fmt.Sprintf("%d", seedTableRowCount)))
		},
	)

	It("postgrescontrollers, integration, postgres-upgrade: blocks an upgrade without the required backup",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.MediumTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			// createPGClassAtVersion configures no backup provider, so the upgrade workflow's
			// rollback baseline can never be established.
			pgClass := createPGClassAtVersion(ctx, kubeClient, ns, pgUpgradeSourceVersion)
			pgCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "upgrade-no-backup", Namespace: ns},
				Spec: platformv1alpha1.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
					PostgresVersion:       ptr.To(pgUpgradeSourceVersion),
					PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{
						Allow:    ptr.To(true),
						Strategy: ptr.To(mvutypes.MajorUpgradeFlowPgUpgrade),
					},
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			clusterKey := types.NamespacedName{Name: pgCluster.Name, Namespace: ns}

			By("waiting for PostgresCluster to reach Ready on the source major version")
			readySource := pgtesthelpers.WaitForReadyPostgresCluster(ctx, kubeClient, clusterKey)
			Expect(readySource.Status.CurrentPgVersion).To(Equal(majorOf(pgUpgradeSourceVersion)))

			cnpgBefore := &cnpgv1.Cluster{}
			Expect(kubeClient.Get(ctx, clusterKey, cnpgBefore)).To(Succeed())
			imageBefore := cnpgBefore.Spec.ImageName

			By("requesting an upgrade to the target major version")
			patch := client.MergeFrom(pgCluster.DeepCopy())
			pgCluster.Spec.PostgresVersion = ptr.To(pgUpgradeTargetVersion)
			Expect(kubeClient.Patch(ctx, pgCluster, patch)).To(Succeed())

			By("asserting the upgrade terminally fails with an actionable BackupProviderMissing reason")
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				entry := currentUpgradeEntry(g, pc)
				g.Expect(entry.Phase).To(HaveValue(Equal(string(mvutypes.Failed))))

				cond := findUpgradeCondition(entry, mvutypes.ConditionMajorUpgradeTerminalFailure)
				g.Expect(cond).NotTo(BeNil(), "expected %s condition, got: %s",
					mvutypes.ConditionMajorUpgradeTerminalFailure, upgradeConditionSummary(entry))
				g.Expect(cond.Reason).To(Equal(mvutypes.ReasonBackupProviderMissing))
				g.Expect(cond.Message).NotTo(BeEmpty())
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying the blocked upgrade left PostgreSQL on the source major version")
			pc := &platformv1alpha1.PostgresCluster{}
			Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
			Expect(pc.Status.CurrentPgVersion).To(Equal(majorOf(pgUpgradeSourceVersion)))

			cnpgAfter := &cnpgv1.Cluster{}
			Expect(kubeClient.Get(ctx, clusterKey, cnpgAfter)).To(Succeed())
			Expect(cnpgAfter.Spec.ImageName).To(Equal(imageBefore), "blocked upgrade must not touch the CNPG image")
		},
	)

	It("postgrescontrollers, integration, postgres-upgrade: blocks a major version change until the upgrade workflow is allowed",
		Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres"),
		NodeTimeout(testenv.MediumTimeout),
		func(ctx SpecContext) {
			ns := testcaseEnvInst.GetName()
			kubeClient := testcaseEnvInst.GetKubeClient()

			pgClass := createPGClassAtVersion(ctx, kubeClient, ns, pgUpgradeSourceVersion)
			pgCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "upgrade-not-allowed", Namespace: ns},
				Spec: platformv1alpha1.PostgresClusterSpec{
					Class:                 pgClass.Name,
					ClusterDeletionPolicy: ptr.To("Delete"),
					PostgresVersion:       ptr.To(pgUpgradeSourceVersion),
				},
			}
			Expect(kubeClient.Create(ctx, pgCluster)).To(Succeed())

			clusterKey := types.NamespacedName{Name: pgCluster.Name, Namespace: ns}

			By("waiting for PostgresCluster to reach Ready on the source major version")
			readySource := pgtesthelpers.WaitForReadyPostgresCluster(ctx, kubeClient, clusterKey)
			Expect(readySource.Status.CurrentPgVersion).To(Equal(majorOf(pgUpgradeSourceVersion)))

			cnpgBefore := &cnpgv1.Cluster{}
			Expect(kubeClient.Get(ctx, clusterKey, cnpgBefore)).To(Succeed())
			imageBefore := cnpgBefore.Spec.ImageName

			By("changing the major version without setting postgresMajorUpgradeConfig.allow")
			patch := client.MergeFrom(pgCluster.DeepCopy())
			pgCluster.Spec.PostgresVersion = ptr.To(pgUpgradeTargetVersion)
			Expect(kubeClient.Patch(ctx, pgCluster, patch)).To(Succeed())

			By("asserting the cluster reports the change is held pending explicit opt-in")
			Eventually(func(g Gomega) {
				pc := &platformv1alpha1.PostgresCluster{}
				g.Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
				condition := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
				g.Expect(condition).NotTo(BeNil(), "cluster conditions: %s", clusterConditionSummary(pc))
				if condition == nil {
					return
				}
				g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(condition.Reason).To(Equal("MajorUpgradeConfigRequired"))
				g.Expect(condition.Message).To(ContainSubstring("postgresMajorUpgradeConfig.allow=true"))
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

			By("verifying no upgrade was started and the CNPG image is unchanged")
			pc := &platformv1alpha1.PostgresCluster{}
			Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
			Expect(pc.Status.PostgresMajorUpgradeStatus).To(BeEmpty(), "no upgrade should be recorded without allow=true")
			Expect(pc.Status.CurrentPgVersion).To(Equal(majorOf(pgUpgradeSourceVersion)))

			cnpgAfter := &cnpgv1.Cluster{}
			Expect(kubeClient.Get(ctx, clusterKey, cnpgAfter)).To(Succeed())
			Expect(cnpgAfter.Spec.ImageName).To(Equal(imageBefore), "held upgrade must not touch the CNPG image")
		},
	)

})

// createPGClassWithSnapshotBackup creates a PostgresClusterClass with CSI volume
// snapshot backups enabled, which the major-version upgrade workflow requires as
// its rollback baseline. Cleanup mirrors createPGClass.
func createPGClassWithSnapshotBackup(ctx SpecContext, kubeClient client.Client, ns, pgVersion string) *platformv1alpha1.PostgresClusterClass {
	GinkgoHelper()
	pgClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "postgres-e2e-backup-" + ns,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "e2e-test",
			},
		},
		Spec: platformv1alpha1.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &platformv1alpha1.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(1)),
				PostgresVersion: ptr.To(pgVersion),
				Backup: &platformv1alpha1.BackupConfig{
					Enabled:  ptr.To(true),
					Schedule: ptr.To("0 2 * * *"),
				},
			},
			CNPG: &platformv1alpha1.CNPGConfig{
				Backup: &platformv1alpha1.CNPGBackupConfig{
					// Single-instance cluster, so there is no standby to prefer.
					Target: ptr.To("primary"),
					VolumeSnapshot: &platformv1alpha1.CNPGVolumeSnapshotConfig{
						ClassName: ptr.To(pgVolumeSnapshotClass),
						Online:    ptr.To(true),
						// Garbage collect snapshots with the CNPG Cluster so the
						// upgrade baselines do not outlive the test namespace.
						SnapshotOwnerReference: ptr.To("cluster"),
					},
				},
			},
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

// currentUpgradeEntry returns the newest major-upgrade history entry, matching
// how the controller appends one entry per upgrade hop.
func currentUpgradeEntry(g Gomega, pc *platformv1alpha1.PostgresCluster) platformv1alpha1.PostgresMajorUpgradeStatus {
	g.Expect(pc.Status.PostgresMajorUpgradeStatus).NotTo(BeEmpty(), "no major upgrade recorded in status")
	return pc.Status.PostgresMajorUpgradeStatus[len(pc.Status.PostgresMajorUpgradeStatus)-1]
}

func findUpgradeCondition(entry platformv1alpha1.PostgresMajorUpgradeStatus, conditionType string) *metav1.Condition {
	for i := range entry.Conditions {
		if entry.Conditions[i].Type == conditionType {
			return &entry.Conditions[i]
		}
	}
	return nil
}

// clusterConditionSummary renders the PostgresCluster conditions so a wait that
// times out on the phase alone reports why the operator is unhappy instead of
// just echoing "Failed".
func clusterConditionSummary(pc *platformv1alpha1.PostgresCluster) string {
	parts := make([]string, 0, len(pc.Status.Conditions))
	for _, c := range pc.Status.Conditions {
		parts = append(parts, fmt.Sprintf("%s=%s/%s (%s)", c.Type, c.Status, c.Reason, c.Message))
	}
	return strings.Join(parts, "; ")
}

func upgradeConditionSummary(entry platformv1alpha1.PostgresMajorUpgradeStatus) string {
	parts := make([]string, 0, len(entry.Conditions))
	for _, c := range entry.Conditions {
		parts = append(parts, fmt.Sprintf("%s=%s/%s (%s)", c.Type, c.Status, c.Reason, c.Message))
	}
	return strings.Join(parts, "; ")
}

func upgradeBackupSummary(ctx SpecContext, kubeClient client.Client, clusterKey types.NamespacedName) string {
	backups := &cnpgv1.BackupList{}
	if err := kubeClient.List(ctx, backups, client.InNamespace(clusterKey.Namespace)); err != nil {
		return fmt.Sprintf("list failed: %v", err)
	}

	parts := make([]string, 0, len(backups.Items))
	for i := range backups.Items {
		backup := &backups.Items[i]
		if backup.Spec.Cluster.Name != clusterKey.Name {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s (%s)", backup.Name, backup.Status.Phase, backup.Status.Error))
	}
	if len(parts) == 0 {
		return "none"
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

// majorOf reduces a spec.postgresVersion ("15" or "15.10") to its major
// component, which is what status.currentPgVersion reports.
func majorOf(version string) string {
	major, _, _ := strings.Cut(version, ".")
	return major
}

// pgCredentials is a username/password pair a psql connection can authenticate
// with, resolved from what the operator publishes rather than reconstructed.
type pgCredentials struct {
	user     string
	password string
}

// managedRoleCredentials resolves the login credentials of a database's managed
// admin and rw roles from PostgresDatabase status. Both the username and the
// password come out of the published Secret, so the test never has to guess role
// names or key conventions.
func managedRoleCredentials(ctx SpecContext, kubeClient client.Client, dbKey types.NamespacedName, database string) (admin, rw pgCredentials) {
	GinkgoHelper()

	pd := &platformv1alpha1.PostgresDatabase{}
	Expect(kubeClient.Get(ctx, dbKey, pd)).To(Succeed())

	var info *platformv1alpha1.DatabaseInfo
	for i := range pd.Status.Databases {
		if pd.Status.Databases[i].Name == database {
			info = &pd.Status.Databases[i]
			break
		}
	}
	Expect(info).NotTo(BeNil(), "PostgresDatabase %s publishes no status for database %q", dbKey.Name, database)

	return readRoleSecret(ctx, kubeClient, dbKey.Namespace, info.AdminUserSecretRef),
		readRoleSecret(ctx, kubeClient, dbKey.Namespace, info.RWUserSecretRef)
}

func readRoleSecret(ctx SpecContext, kubeClient client.Client, ns string, ref *corev1.SecretKeySelector) pgCredentials {
	GinkgoHelper()
	Expect(ref).NotTo(BeNil(), "PostgresDatabase status has no managed role Secret reference")

	secret := &corev1.Secret{}
	Expect(kubeClient.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, secret)).To(Succeed())

	passwordKey := ref.Key
	if passwordKey == "" {
		passwordKey = "password"
	}
	user, ok := secret.Data["username"]
	Expect(ok).To(BeTrue(), "managed role Secret %s has no username key", ref.Name)
	password, ok := secret.Data[passwordKey]
	Expect(ok).To(BeTrue(), "managed role Secret %s has no %q key", ref.Name, passwordKey)

	return pgCredentials{user: string(user), password: string(password)}
}

// runSQL executes statements as the cluster superuser advertised in
// PostgresCluster status. See runSQLAs for the connection mechanics.
func runSQL(ctx SpecContext, kubeClient client.Client, deployment *testenv.Deployment, clusterKey types.NamespacedName, database, sql string) string {
	GinkgoHelper()

	pc := &platformv1alpha1.PostgresCluster{}
	Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())
	Expect(pc.Status.Resources).NotTo(BeNil(), "PostgresCluster status has no resource references")
	Expect(pc.Status.Resources.SuperUserSecretRef).NotTo(BeNil(), "PostgresCluster status has no superuser Secret")

	user := connectionConfigMap(ctx, kubeClient, clusterKey, pc).Data["SUPER_USER_NAME"]
	Expect(user).NotTo(BeEmpty(), "connection ConfigMap has no SUPER_USER_NAME")

	secret := &corev1.Secret{}
	Expect(kubeClient.Get(ctx, types.NamespacedName{
		Name:      pc.Status.Resources.SuperUserSecretRef.Name,
		Namespace: clusterKey.Namespace,
	}, secret)).To(Succeed())

	passwordKey := pc.Status.Resources.SuperUserSecretRef.Key
	if passwordKey == "" {
		passwordKey = "password"
	}
	password, ok := secret.Data[passwordKey]
	Expect(ok).To(BeTrue(), "superuser Secret %s has no %q key", secret.Name, passwordKey)

	return runSQLAs(ctx, kubeClient, deployment, clusterKey, pgCredentials{user: user, password: string(password)}, database, sql)
}

func connectionConfigMap(ctx SpecContext, kubeClient client.Client, clusterKey types.NamespacedName, pc *platformv1alpha1.PostgresCluster) *corev1.ConfigMap {
	GinkgoHelper()
	Expect(pc.Status.Resources).NotTo(BeNil(), "PostgresCluster status has no resource references")
	Expect(pc.Status.Resources.ConfigMapRef).NotTo(BeNil(), "PostgresCluster status has no connection ConfigMap")

	connInfo := &corev1.ConfigMap{}
	Expect(kubeClient.Get(ctx, types.NamespacedName{
		Name:      pc.Status.Resources.ConfigMapRef.Name,
		Namespace: clusterKey.Namespace,
	}, connInfo)).To(Succeed())
	return connInfo
}

// runSQLAs executes statements against the given database through the cluster's
// published RW endpoint as creds. psql runs inside the CNPG primary pod because
// that is the only pod guaranteed to have a client; the connection itself still
// goes through the Service, so a failure here means the endpoint is unusable.
// Returns trimmed stdout, so single-value queries come back as a bare value.
func runSQLAs(ctx SpecContext, kubeClient client.Client, deployment *testenv.Deployment, clusterKey types.NamespacedName, creds pgCredentials, database, sql string) string {
	GinkgoHelper()

	pc := &platformv1alpha1.PostgresCluster{}
	Expect(kubeClient.Get(ctx, clusterKey, pc)).To(Succeed())

	connInfo := connectionConfigMap(ctx, kubeClient, clusterKey, pc)
	endpoint := connInfo.Data["CLUSTER_RW_ENDPOINT"]
	port := connInfo.Data["DEFAULT_CLUSTER_PORT"]
	Expect(endpoint).NotTo(BeEmpty(), "connection ConfigMap has no CLUSTER_RW_ENDPOINT")
	Expect(port).NotTo(BeEmpty(), "connection ConfigMap has no DEFAULT_CLUSTER_PORT")
	Expect(creds.user).NotTo(BeEmpty(), "no username to connect as")

	dsn := fmt.Sprintf("postgresql://%s@%s:%s/%s?sslmode=require", creds.user, endpoint, port, database)

	// Credentials and SQL both travel on stdin rather than argv so the password
	// never lands in the pod's process list. The heredoc is quoted, so the shell
	// performs no expansion on the SQL body.
	script := fmt.Sprintf(`set -e
export PGPASSWORD='%s'
psql '%s' --no-psqlrc --tuples-only --no-align --quiet --single-transaction -v ON_ERROR_STOP=1 <<'SPLUNK_E2E_SQL'
%s
SPLUNK_E2E_SQL
`, shellSingleQuoteEscape(creds.password), shellSingleQuoteEscape(dsn), sql)

	resolvePrimary := func(attemptCtx context.Context) (string, error) {
		cnpg := &cnpgv1.Cluster{}
		if err := kubeClient.Get(attemptCtx, clusterKey, cnpg); err != nil {
			return "", fmt.Errorf("getting CNPG Cluster primary: %w", err)
		}
		if cnpg.Status.CurrentPrimary == "" {
			return "", fmt.Errorf("CNPG Cluster %s has no current primary", clusterKey)
		}
		return cnpg.Status.CurrentPrimary, nil
	}

	stdout, stderr, err := pgtesthelpers.ExecutePostgresPodCommand(ctx, deployment, resolvePrimary, []string{"/bin/sh"}, script)
	Expect(err).To(Succeed(), "psql exec failed: %s", stderr)
	return strings.TrimSpace(stdout)
}

// shellSingleQuoteEscape makes a value safe to embed inside single quotes in a
// POSIX shell by closing, escaping, and reopening the quoted run.
func shellSingleQuoteEscape(value string) string {
	return strings.ReplaceAll(value, `'`, `'\''`)
}
