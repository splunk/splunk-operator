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
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgtesthelpers "github.com/splunk/splunk-operator/test/postgrescontrollers/helpers"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const backupRestoreDatabaseName = "appdb"

var _ = Describe("postgrescontrollers, integration, postgres-snapshot-backup-restore",
	Label("tier:e2e-full", "cloud:aws", "feature:postgres-snapshot"), func() {
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

		It("backs up a database with CSI snapshots and restores an independent cluster",
			Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:postgres-snapshot"),
			NodeTimeout(testenv.MediumLongTimeout),
			func(ctx SpecContext) {
				namespace := testcaseEnvInst.GetName()
				snapshotClassName := os.Getenv("POSTGRES_E2E_VOLUME_SNAPSHOT_CLASS")
				Expect(snapshotClassName).NotTo(BeEmpty(), "set POSTGRES_E2E_VOLUME_SNAPSHOT_CLASS to the PGDATA CSI snapshot class")

				snapshots, snapshotDriver := pgtesthelpers.RequireVolumeSnapshotClass(ctx, snapshotClassName)

				apiClient, err := pgtesthelpers.NewDirectPostgresClient()
				Expect(err).To(Succeed())

				schedule := "* * * * *"
				class := &platformv1alpha1.PostgresClusterClass{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "postgres-snapshot-" + namespace,
						Labels: map[string]string{"app.kubernetes.io/managed-by": "e2e-test"},
					},
					Spec: platformv1alpha1.PostgresClusterClassSpec{
						Provisioner: "postgresql.cnpg.io",
						Config: &platformv1alpha1.PostgresClusterClassConfig{
							Instances: ptr.To(int32(1)),
							Backup: &platformv1alpha1.BackupConfig{
								Enabled:  ptr.To(true),
								Schedule: &schedule,
							},
						},
						CNPG: &platformv1alpha1.CNPGConfig{Backup: &platformv1alpha1.CNPGBackupConfig{
							Target: ptr.To("primary"),
							VolumeSnapshot: &platformv1alpha1.CNPGVolumeSnapshotConfig{
								ClassName:              &snapshotClassName,
								SnapshotOwnerReference: ptr.To("cluster"),
								Online:                 ptr.To(true),
							},
						}},
					},
				}
				Expect(apiClient.Create(ctx, class)).To(Succeed())
				DeferCleanup(func(ctx SpecContext) {
					err := apiClient.Delete(ctx, class)
					if err != nil && !apierrors.IsNotFound(err) {
						Expect(err).To(Succeed(), "failed to clean up snapshot PostgresClusterClass")
					}
				})
				pgtesthelpers.RegisterSnapshotFailureDump(apiClient, snapshots, namespace)

				source := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "snapshot-source", Namespace: namespace},
					Spec: platformv1alpha1.PostgresClusterSpec{
						Class:                 class.Name,
						ClusterDeletionPolicy: ptr.To("Delete"),
					},
				}
				Expect(apiClient.Create(ctx, source)).To(Succeed())
				sourceKey := types.NamespacedName{Name: source.Name, Namespace: namespace}

				By("waiting for the source cluster and snapshot schedule")
				source = pgtesthelpers.WaitForReadyPostgresCluster(ctx, apiClient, sourceKey)
				sourceSuperuserSecretUID := pgtesthelpers.PostgresClusterSuperuserSecretUID(ctx, apiClient, source)
				primaryPVC := &corev1.PersistentVolumeClaim{}
				Expect(apiClient.Get(ctx, types.NamespacedName{Name: *source.Status.CurrentPrimary, Namespace: namespace}, primaryPVC)).To(Succeed())
				Expect(primaryPVC.Spec.VolumeName).NotTo(BeEmpty())
				primaryPV := &corev1.PersistentVolume{}
				Expect(apiClient.Get(ctx, types.NamespacedName{Name: primaryPVC.Spec.VolumeName}, primaryPV)).To(Succeed())
				effectiveDriver := primaryPV.Annotations["pv.kubernetes.io/migrated-to"]
				if primaryPV.Spec.CSI != nil {
					effectiveDriver = primaryPV.Spec.CSI.Driver
				}
				if effectiveDriver != "" {
					Expect(effectiveDriver).To(Equal(snapshotDriver),
						"VolumeSnapshotClass driver must match the source PGDATA volume")
				} else {
					fmt.Fprintf(GinkgoWriter,
						"PGDATA PV %s exposes no CSI or migrated-to driver; the snapshot operation remains authoritative\n",
						primaryPV.Name)
				}
				Eventually(func(g Gomega) {
					current := &platformv1alpha1.PostgresCluster{}
					g.Expect(apiClient.Get(ctx, sourceKey, current)).To(Succeed())
					pgtesthelpers.StopIfPostgresClusterFailed(current)
					condition := meta.FindStatusCondition(current.Status.Conditions, "BackupReady")
					g.Expect(condition).NotTo(BeNil())
					if condition == nil {
						return
					}
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(condition.Reason).To(Equal("BackupConfigured"))
					g.Expect(condition.ObservedGeneration).To(Equal(current.Generation))
					g.Expect(current.Status.BackupStatus).NotTo(BeNil())
					if current.Status.BackupStatus == nil {
						return
					}
					g.Expect(current.Status.BackupStatus.VolumeSnapshot).NotTo(BeNil())
					if current.Status.BackupStatus.VolumeSnapshot == nil {
						return
					}
					g.Expect(current.Status.BackupStatus.VolumeSnapshot.Enabled).To(BeTrue())
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				sourceCNPG := &cnpgv1.Cluster{}
				Eventually(func(g Gomega) {
					current := &platformv1alpha1.PostgresCluster{}
					g.Expect(apiClient.Get(ctx, sourceKey, current)).To(Succeed())
					pgtesthelpers.StopIfPostgresClusterFailed(current)
					g.Expect(apiClient.Get(ctx, sourceKey, sourceCNPG)).To(Succeed())
					g.Expect(sourceCNPG.Status.Phase).To(Equal(cnpgv1.PhaseHealthy))
					g.Expect(sourceCNPG.Spec.Backup).NotTo(BeNil())
					if sourceCNPG.Spec.Backup == nil {
						return
					}
					g.Expect(sourceCNPG.Spec.Backup.Target).To(Equal(cnpgv1.BackupTargetPrimary))
					g.Expect(sourceCNPG.Spec.Backup.VolumeSnapshot).NotTo(BeNil())
					if sourceCNPG.Spec.Backup.VolumeSnapshot == nil {
						return
					}
					g.Expect(sourceCNPG.Spec.Backup.VolumeSnapshot.ClassName).To(Equal(snapshotClassName))
					g.Expect(string(sourceCNPG.Spec.Backup.VolumeSnapshot.SnapshotOwnerReference)).To(Equal("cluster"))
					g.Expect(sourceCNPG.Spec.Backup.VolumeSnapshot.Online).To(HaveValue(BeTrue()))
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				scheduledKey := types.NamespacedName{Name: source.Name + "-backup", Namespace: namespace}
				Eventually(func(g Gomega) {
					current := &platformv1alpha1.PostgresCluster{}
					g.Expect(apiClient.Get(ctx, sourceKey, current)).To(Succeed())
					pgtesthelpers.StopIfPostgresClusterFailed(current)
					scheduled := &cnpgv1.ScheduledBackup{}
					g.Expect(apiClient.Get(ctx, scheduledKey, scheduled)).To(Succeed())
					g.Expect(scheduled.Spec.Cluster.Name).To(Equal(source.Name))
					g.Expect(scheduled.Spec.Method).To(Equal(cnpgv1.BackupMethodVolumeSnapshot))
					g.Expect(scheduled.Spec.Target).To(Equal(cnpgv1.BackupTargetPrimary))
					g.Expect(scheduled.Spec.Schedule).To(Equal("0 * * * * *"))
					g.Expect(scheduled.Spec.BackupOwnerReference).To(Equal("cluster"))
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				sourceDatabase := pgtesthelpers.CreateReadyPostgresDatabase(
					ctx, apiClient, namespace, "snapshot-source-db", source.Name, backupRestoreDatabaseName,
				)
				sourceSecretUIDs := pgtesthelpers.DatabaseSecretUIDs(ctx, apiClient, sourceDatabase)
				sourceDatabaseChildren := pgtesthelpers.PostgresDatabaseChildResources(sourceDatabase)
				pgtesthelpers.ExpectPostgresDatabaseChildrenPresent(ctx, apiClient, sourceDatabaseChildren)

				By("writing the restore-boundary fixture")
				_, err = pgtesthelpers.ExecutePostgresSQLInDatabase(ctx, apiClient, deployment, sourceKey, backupRestoreDatabaseName, `
CREATE TABLE IF NOT EXISTS restore_probe (id integer PRIMARY KEY, value text NOT NULL);
INSERT INTO restore_probe (id, value) VALUES (1, 'before-backup')
ON CONFLICT (id) DO UPDATE SET value = EXCLUDED.value;`)
				Expect(err).To(Succeed())

				baselineUIDs, err := pgtesthelpers.SnapshotBackupUIDs(ctx, apiClient, namespace, source.Name)
				Expect(err).To(Succeed())
				baselineCluster := &platformv1alpha1.PostgresCluster{}
				Expect(apiClient.Get(ctx, sourceKey, baselineCluster)).To(Succeed())
				var baselineLastSchedule *metav1.Time
				if baselineCluster.Status.BackupStatus != nil && baselineCluster.Status.BackupStatus.VolumeSnapshot != nil {
					if last := baselineCluster.Status.BackupStatus.VolumeSnapshot.LastScheduleTime; last != nil {
						baselineLastSchedule = last.DeepCopy()
					}
				}

				By("waiting for a newly scheduled snapshot backup")
				backup := pgtesthelpers.WaitForNewCompletedSnapshotBackup(
					ctx, apiClient, namespace, source.Name, sourceCNPG.UID, baselineUIDs,
				)
				Expect(backup.Status.BackupSnapshotStatus.Elements).To(HaveLen(1))
				snapshotElement := backup.Status.BackupSnapshotStatus.Elements[0]
				Expect(snapshotElement.Type).To(Equal("PG_DATA"))
				Expect(snapshotElement.Name).NotTo(BeEmpty())

				Eventually(func(g Gomega) {
					current := &platformv1alpha1.PostgresCluster{}
					g.Expect(apiClient.Get(ctx, sourceKey, current)).To(Succeed())
					pgtesthelpers.StopIfPostgresClusterFailed(current)
					g.Expect(current.Status.BackupStatus).NotTo(BeNil())
					if current.Status.BackupStatus == nil {
						return
					}
					status := current.Status.BackupStatus.VolumeSnapshot
					g.Expect(status).NotTo(BeNil())
					if status == nil {
						return
					}
					g.Expect(status.LastScheduleTime).NotTo(BeNil())
					if status.LastScheduleTime == nil {
						return
					}
					if baselineLastSchedule != nil {
						g.Expect(status.LastScheduleTime.Equal(baselineLastSchedule)).To(BeFalse())
					}
					g.Expect(status.NextScheduleTime).NotTo(BeNil())
					if status.NextScheduleTime == nil {
						return
					}
					g.Expect(status.NextScheduleTime.After(status.LastScheduleTime.Time)).To(BeTrue())
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				By("verifying the exact PGDATA VolumeSnapshot is ready")
				var snapshotContentName string
				Eventually(func(g Gomega) {
					snapshot, getErr := snapshots.SnapshotV1().VolumeSnapshots(namespace).Get(ctx, snapshotElement.Name, metav1.GetOptions{})
					g.Expect(getErr).To(Succeed())
					g.Expect(snapshot.Spec.VolumeSnapshotClassName).To(HaveValue(Equal(snapshotClassName)))
					g.Expect(snapshot.Status).NotTo(BeNil())
					if snapshot.Status == nil {
						return
					}
					if snapshot.Status.Error != nil {
						StopTrying(fmt.Sprintf("VolumeSnapshot %s failed: %v", snapshot.Name, snapshot.Status.Error)).Now()
					}
					g.Expect(snapshot.Status.Error).To(BeNil())
					g.Expect(snapshot.Status.BoundVolumeSnapshotContentName).NotTo(BeNil())
					g.Expect(snapshot.Status.ReadyToUse).To(HaveValue(BeTrue()))
					if snapshot.Status.BoundVolumeSnapshotContentName == nil {
						return
					}
					snapshotContentName = *snapshot.Status.BoundVolumeSnapshotContentName
					owner := metav1.GetControllerOf(snapshot)
					g.Expect(owner).NotTo(BeNil())
					if owner == nil {
						return
					}
					g.Expect(owner.UID).To(Equal(sourceCNPG.UID))
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				By("disabling future source backups after selecting the restore point")
				currentSource := &platformv1alpha1.PostgresCluster{}
				Expect(apiClient.Get(ctx, sourceKey, currentSource)).To(Succeed())
				patch := client.MergeFrom(currentSource.DeepCopy())
				currentSource.Spec.Backup = &platformv1alpha1.BackupConfig{Enabled: ptr.To(false)}
				Expect(apiClient.Patch(ctx, currentSource, patch)).To(Succeed())
				Eventually(func(g Gomega) {
					current := &platformv1alpha1.PostgresCluster{}
					g.Expect(apiClient.Get(ctx, sourceKey, current)).To(Succeed())
					pgtesthelpers.StopIfPostgresClusterFailed(current)
					condition := meta.FindStatusCondition(current.Status.Conditions, "BackupReady")
					g.Expect(condition).NotTo(BeNil())
					if condition == nil {
						return
					}
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(condition.Reason).To(Equal("BackupDisabled"))
					err := apiClient.Get(ctx, scheduledKey, &cnpgv1.ScheduledBackup{})
					g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				_, err = pgtesthelpers.ExecutePostgresSQLInDatabase(ctx, apiClient, deployment, sourceKey, backupRestoreDatabaseName,
					`INSERT INTO restore_probe (id, value) VALUES (2, 'source-after-backup')
ON CONFLICT (id) DO UPDATE SET value = EXCLUDED.value`)
				Expect(err).To(Succeed())

				restoreStarted := time.Now()
				restored := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "snapshot-restored", Namespace: namespace},
					Spec: platformv1alpha1.PostgresClusterSpec{
						Class:                 class.Name,
						ClusterDeletionPolicy: ptr.To("Delete"),
						Backup:                &platformv1alpha1.BackupConfig{Enabled: ptr.To(false)},
						BootstrapFrom: &platformv1alpha1.BootstrapFrom{
							VolumeSnapshot: &platformv1alpha1.VolumeSnapshotSource{Storage: snapshotElement.Name},
						},
					},
				}
				Expect(apiClient.Create(ctx, restored)).To(Succeed())
				restoredKey := types.NamespacedName{Name: restored.Name, Namespace: namespace}

				By("waiting for the restored cluster and credential sweep")
				restored = pgtesthelpers.WaitForReadyPostgresCluster(ctx, apiClient, restoredKey)
				fmt.Fprintf(GinkgoWriter, "CSI snapshot restore became ready in %s\n", time.Since(restoreStarted))
				restoredPrimaryPVC := &corev1.PersistentVolumeClaim{}
				Expect(apiClient.Get(ctx, types.NamespacedName{Name: *restored.Status.CurrentPrimary, Namespace: namespace}, restoredPrimaryPVC)).To(Succeed())
				Expect(pgtesthelpers.PostgresClusterSuperuserSecretUID(ctx, apiClient, restored)).NotTo(Equal(sourceSuperuserSecretUID),
					"restored cluster must use a fresh superuser Secret identity")
				Expect(restored.Status.Restore).NotTo(BeNil())
				Expect(restored.Status.Restore.Source.VolumeSnapshot).To(HaveValue(Equal(snapshotElement.Name)))
				Expect(restored.Status.Restore.Source.ObjectStorage).To(BeNil())
				Expect(restored.Status.Restore.CredentialSweep.Completed).To(BeTrue())
				condition := meta.FindStatusCondition(restored.Status.Conditions, "BackupReady")
				Expect(condition).NotTo(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				Expect(condition.Reason).To(Equal("BackupDisabled"))
				err = apiClient.Get(ctx, types.NamespacedName{Name: restored.Name + "-backup", Namespace: namespace}, &cnpgv1.ScheduledBackup{})
				Expect(apierrors.IsNotFound(err)).To(BeTrue())

				restoredDatabase := pgtesthelpers.CreateReadyPostgresDatabase(
					ctx, apiClient, namespace, "snapshot-restored-db", restored.Name, backupRestoreDatabaseName,
				)
				restoredSecretUIDs := pgtesthelpers.DatabaseSecretUIDs(ctx, apiClient, restoredDatabase)
				restoredDatabaseChildren := pgtesthelpers.PostgresDatabaseChildResources(restoredDatabase)
				pgtesthelpers.ExpectPostgresDatabaseChildrenPresent(ctx, apiClient, restoredDatabaseChildren)
				for uid := range restoredSecretUIDs {
					_, reused := sourceSecretUIDs[uid]
					Expect(reused).To(BeFalse(), "restored database must use fresh Secret identities")
				}

				By("proving the restore boundary")
				rows, err := pgtesthelpers.ExecutePostgresSQLInDatabase(ctx, apiClient, deployment, restoredKey, backupRestoreDatabaseName,
					`SELECT id || ':' || value FROM restore_probe ORDER BY id`)
				Expect(err).To(Succeed())
				Expect(rows).To(Equal("1:before-backup"))

				_, err = pgtesthelpers.ExecutePostgresSQLInDatabase(ctx, apiClient, deployment, restoredKey, backupRestoreDatabaseName,
					`INSERT INTO restore_probe (id, value) VALUES (3, 'restored-after-backup')
ON CONFLICT (id) DO UPDATE SET value = EXCLUDED.value`)
				Expect(err).To(Succeed())
				sourceRows, err := pgtesthelpers.ExecutePostgresSQLInDatabase(ctx, apiClient, deployment, sourceKey, backupRestoreDatabaseName,
					`SELECT id || ':' || value FROM restore_probe ORDER BY id`)
				Expect(err).To(Succeed())
				Expect(sourceRows).To(Equal("1:before-backup\n2:source-after-backup"))
				restoredRows, err := pgtesthelpers.ExecutePostgresSQLInDatabase(ctx, apiClient, deployment, restoredKey, backupRestoreDatabaseName,
					`SELECT id || ':' || value FROM restore_probe ORDER BY id`)
				Expect(err).To(Succeed())
				Expect(restoredRows).To(Equal("1:before-backup\n3:restored-after-backup"))

				By("deleting the source and proving its selected backup resources existed before cleanup")
				Expect(apiClient.Get(ctx, types.NamespacedName{Name: backup.Name, Namespace: namespace}, &cnpgv1.Backup{})).To(Succeed())
				_, err = snapshots.SnapshotV1().VolumeSnapshots(namespace).Get(ctx, snapshotElement.Name, metav1.GetOptions{})
				Expect(err).To(Succeed())
				Expect(apiClient.Delete(ctx, sourceDatabase)).To(Succeed())
				Eventually(func() error {
					return apiClient.Get(ctx, types.NamespacedName{Name: sourceDatabase.Name, Namespace: namespace}, &platformv1alpha1.PostgresDatabase{})
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Satisfy(apierrors.IsNotFound))
				pgtesthelpers.ExpectPostgresDatabaseChildrenDeleted(ctx, apiClient, sourceDatabaseChildren)
				Expect(apiClient.Delete(ctx, currentSource)).To(Succeed())
				Eventually(func(g Gomega) {
					g.Expect(apierrors.IsNotFound(apiClient.Get(ctx, sourceKey, &platformv1alpha1.PostgresCluster{}))).To(BeTrue())
					g.Expect(apierrors.IsNotFound(apiClient.Get(ctx, sourceKey, &cnpgv1.Cluster{}))).To(BeTrue())
					g.Expect(apierrors.IsNotFound(apiClient.Get(ctx, types.NamespacedName{Name: primaryPVC.Name, Namespace: namespace}, &corev1.PersistentVolumeClaim{}))).To(BeTrue())
					g.Expect(apierrors.IsNotFound(apiClient.Get(ctx, types.NamespacedName{Name: backup.Name, Namespace: namespace}, &cnpgv1.Backup{}))).To(BeTrue())
					_, getErr := snapshots.SnapshotV1().VolumeSnapshots(namespace).Get(ctx, snapshotElement.Name, metav1.GetOptions{})
					g.Expect(apierrors.IsNotFound(getErr)).To(BeTrue())
					_, getErr = snapshots.SnapshotV1().VolumeSnapshotContents().Get(ctx, snapshotContentName, metav1.GetOptions{})
					g.Expect(apierrors.IsNotFound(getErr)).To(BeTrue())
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())

				rows, err = pgtesthelpers.ExecutePostgresSQLInDatabase(ctx, apiClient, deployment, restoredKey, backupRestoreDatabaseName,
					`SELECT id || ':' || value FROM restore_probe ORDER BY id`)
				Expect(err).To(Succeed())
				Expect(rows).To(Equal("1:before-backup\n3:restored-after-backup"))

				By("cleaning up the restored database and cluster")
				Expect(apiClient.Delete(ctx, restoredDatabase)).To(Succeed())
				Eventually(func() error {
					return apiClient.Get(ctx, types.NamespacedName{Name: restoredDatabase.Name, Namespace: namespace}, &platformv1alpha1.PostgresDatabase{})
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Satisfy(apierrors.IsNotFound))
				pgtesthelpers.ExpectPostgresDatabaseChildrenDeleted(ctx, apiClient, restoredDatabaseChildren)
				Expect(apiClient.Delete(ctx, restored)).To(Succeed())
				Eventually(func(g Gomega) {
					g.Expect(apierrors.IsNotFound(apiClient.Get(ctx, restoredKey, &platformv1alpha1.PostgresCluster{}))).To(BeTrue())
					g.Expect(apierrors.IsNotFound(apiClient.Get(ctx, restoredKey, &cnpgv1.Cluster{}))).To(BeTrue())
					g.Expect(apierrors.IsNotFound(apiClient.Get(ctx, types.NamespacedName{Name: restoredPrimaryPVC.Name, Namespace: namespace}, &corev1.PersistentVolumeClaim{}))).To(BeTrue())
				}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
			},
		)
	})
