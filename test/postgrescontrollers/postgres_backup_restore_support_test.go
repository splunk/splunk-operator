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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	snapshotclient "github.com/kubernetes-csi/external-snapshotter/client/v8/clientset/versioned"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const backupRestoreDatabaseName = "appdb"

const (
	postgresExecAttemptTimeout = 40 * time.Second
	postgresExecRetryTimeout   = 2 * time.Minute
)

type postgresDatabaseChildren struct {
	database  types.NamespacedName
	configMap types.NamespacedName
	secrets   []types.NamespacedName
}

func newDirectPostgresClient() (client.Client, error) {
	restConfig, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	if err := kubescheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := enterprisev4.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := cnpgv1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	return client.New(restConfig, client.Options{Scheme: scheme})
}

func waitForReadyPostgresCluster(ctx context.Context, kubeClient client.Client, key types.NamespacedName) *enterprisev4.PostgresCluster {
	var ready *enterprisev4.PostgresCluster
	Eventually(func(g Gomega) {
		current := &enterprisev4.PostgresCluster{}
		g.Expect(kubeClient.Get(ctx, key, current)).To(Succeed())
		stopIfPostgresClusterFailed(current)
		g.Expect(current.Status.Phase).To(HaveValue(Equal("Ready")))
		g.Expect(current.Status.CurrentPrimary).NotTo(BeNil())
		ready = current.DeepCopy()
	}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
	return ready
}

func postgresClusterFailure(cluster *enterprisev4.PostgresCluster) string {
	failures := make([]string, 0, len(cluster.Status.Conditions))
	for _, condition := range cluster.Status.Conditions {
		if condition.Status == metav1.ConditionFalse {
			failures = append(failures, fmt.Sprintf("%s/%s: %s", condition.Type, condition.Reason, condition.Message))
		}
	}
	if len(failures) == 0 {
		failures = append(failures, "no failing condition was reported")
	}
	return fmt.Sprintf("PostgresCluster %s/%s entered Failed: %s", cluster.Namespace, cluster.Name, strings.Join(failures, "; "))
}

func stopIfPostgresClusterFailed(cluster *enterprisev4.PostgresCluster) {
	if cluster.Status.Phase != nil && *cluster.Status.Phase == "Failed" {
		StopTrying(postgresClusterFailure(cluster)).Now()
	}
}

func createReadyPostgresDatabase(
	ctx context.Context,
	kubeClient client.Client,
	namespace, name, clusterName string,
) *enterprisev4.PostgresDatabase {
	database := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: clusterName},
			Databases: []enterprisev4.DatabaseDefinition{{
				Name:           backupRestoreDatabaseName,
				DeletionPolicy: "Delete",
			}},
		},
	}
	Expect(kubeClient.Create(ctx, database)).To(Succeed())

	key := types.NamespacedName{Name: name, Namespace: namespace}
	var ready *enterprisev4.PostgresDatabase
	Eventually(func(g Gomega) {
		current := &enterprisev4.PostgresDatabase{}
		g.Expect(kubeClient.Get(ctx, key, current)).To(Succeed())
		if current.Status.Phase != nil && *current.Status.Phase == "Failed" {
			StopTrying(postgresDatabaseFailure(current)).Now()
		}
		g.Expect(current.Status.Phase).To(HaveValue(Equal("Ready")))
		g.Expect(current.Status.Databases).To(HaveLen(1))
		if len(current.Status.Databases) != 1 {
			return
		}
		g.Expect(current.Status.Databases[0].Name).To(Equal(backupRestoreDatabaseName))
		g.Expect(current.Status.Databases[0].AdminUserSecretRef).NotTo(BeNil())
		g.Expect(current.Status.Databases[0].RWUserSecretRef).NotTo(BeNil())
		ready = current.DeepCopy()
	}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
	return ready
}

func postgresDatabaseFailure(database *enterprisev4.PostgresDatabase) string {
	failures := make([]string, 0, len(database.Status.Conditions))
	for _, condition := range database.Status.Conditions {
		if condition.Status == metav1.ConditionFalse {
			failures = append(failures, fmt.Sprintf("%s/%s: %s", condition.Type, condition.Reason, condition.Message))
		}
	}
	if len(failures) == 0 {
		failures = append(failures, "no failing condition was reported")
	}
	return fmt.Sprintf("PostgresDatabase %s/%s entered Failed: %s", database.Namespace, database.Name, strings.Join(failures, "; "))
}

func databaseSecretUIDs(ctx context.Context, kubeClient client.Client, database *enterprisev4.PostgresDatabase) map[types.UID]struct{} {
	result := make(map[types.UID]struct{}, 2)
	for _, ref := range []*corev1.SecretKeySelector{
		database.Status.Databases[0].AdminUserSecretRef,
		database.Status.Databases[0].RWUserSecretRef,
	} {
		secret := &corev1.Secret{}
		Expect(kubeClient.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: database.Namespace}, secret)).To(Succeed())
		result[secret.UID] = struct{}{}
	}
	return result
}

func postgresDatabaseChildResources(database *enterprisev4.PostgresDatabase) postgresDatabaseChildren {
	Expect(database.Status.Databases).To(HaveLen(1))
	status := database.Status.Databases[0]
	Expect(status.DatabaseRef).NotTo(BeNil())
	Expect(status.ConfigMapRef).NotTo(BeNil())
	Expect(status.AdminUserSecretRef).NotTo(BeNil())
	Expect(status.RWUserSecretRef).NotTo(BeNil())

	return postgresDatabaseChildren{
		database:  types.NamespacedName{Name: status.DatabaseRef.Name, Namespace: database.Namespace},
		configMap: types.NamespacedName{Name: status.ConfigMapRef.Name, Namespace: database.Namespace},
		secrets: []types.NamespacedName{
			{Name: status.AdminUserSecretRef.Name, Namespace: database.Namespace},
			{Name: status.RWUserSecretRef.Name, Namespace: database.Namespace},
		},
	}
}

func expectPostgresDatabaseChildrenPresent(ctx context.Context, kubeClient client.Client, children postgresDatabaseChildren) {
	Eventually(func(g Gomega) {
		g.Expect(kubeClient.Get(ctx, children.database, &cnpgv1.Database{})).To(Succeed())
		g.Expect(kubeClient.Get(ctx, children.configMap, &corev1.ConfigMap{})).To(Succeed())
		for _, key := range children.secrets {
			g.Expect(kubeClient.Get(ctx, key, &corev1.Secret{})).To(Succeed())
		}
	}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
}

func expectPostgresDatabaseChildrenDeleted(ctx context.Context, kubeClient client.Client, children postgresDatabaseChildren) {
	Eventually(func(g Gomega) {
		g.Expect(apierrors.IsNotFound(kubeClient.Get(ctx, children.database, &cnpgv1.Database{}))).To(BeTrue())
		g.Expect(apierrors.IsNotFound(kubeClient.Get(ctx, children.configMap, &corev1.ConfigMap{}))).To(BeTrue())
		for _, key := range children.secrets {
			g.Expect(apierrors.IsNotFound(kubeClient.Get(ctx, key, &corev1.Secret{}))).To(BeTrue())
		}
	}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
}

func postgresClusterSuperuserSecretUID(
	ctx context.Context,
	kubeClient client.Client,
	cluster *enterprisev4.PostgresCluster,
) types.UID {
	Expect(cluster.Status.Resources).NotTo(BeNil())
	Expect(cluster.Status.Resources.SuperUserSecretRef).NotTo(BeNil())

	secret := &corev1.Secret{}
	Expect(kubeClient.Get(ctx, types.NamespacedName{
		Name:      cluster.Status.Resources.SuperUserSecretRef.Name,
		Namespace: cluster.Namespace,
	}, secret)).To(Succeed())
	return secret.UID
}

func registerBackupRestoreFailureDump(kubeClient client.Client, snapshots snapshotclient.Interface, namespace string) {
	DeferCleanup(func(ctx SpecContext) {
		if !CurrentSpecReport().Failed() {
			return
		}

		clusters := &enterprisev4.PostgresClusterList{}
		if err := kubeClient.List(ctx, clusters, client.InNamespace(namespace)); err != nil {
			fmt.Fprintf(GinkgoWriter, "backup/restore diagnostics: listing PostgresClusters: %v\n", err)
		} else {
			for i := range clusters.Items {
				cluster := &clusters.Items[i]
				fmt.Fprintf(GinkgoWriter, "PostgresCluster %s phase=%v backup=%+v restore=%+v\n",
					cluster.Name, cluster.Status.Phase, cluster.Status.BackupStatus, cluster.Status.Restore)
				for _, condition := range cluster.Status.Conditions {
					fmt.Fprintf(GinkgoWriter, "  condition type=%s status=%s reason=%s observedGeneration=%d message=%q\n",
						condition.Type, condition.Status, condition.Reason, condition.ObservedGeneration, condition.Message)
				}
			}
		}

		cnpgClusters := &cnpgv1.ClusterList{}
		if err := kubeClient.List(ctx, cnpgClusters, client.InNamespace(namespace)); err != nil {
			fmt.Fprintf(GinkgoWriter, "backup/restore diagnostics: listing CNPG Clusters: %v\n", err)
		} else {
			for i := range cnpgClusters.Items {
				cluster := &cnpgClusters.Items[i]
				fmt.Fprintf(GinkgoWriter, "CNPG Cluster %s phase=%s currentPrimary=%s\n",
					cluster.Name, cluster.Status.Phase, cluster.Status.CurrentPrimary)
			}
		}

		schedules := &cnpgv1.ScheduledBackupList{}
		if err := kubeClient.List(ctx, schedules, client.InNamespace(namespace)); err != nil {
			fmt.Fprintf(GinkgoWriter, "backup/restore diagnostics: listing ScheduledBackups: %v\n", err)
		} else {
			for i := range schedules.Items {
				schedule := &schedules.Items[i]
				fmt.Fprintf(GinkgoWriter, "ScheduledBackup %s method=%s cluster=%s last=%v next=%v\n",
					schedule.Name, schedule.Spec.Method, schedule.Spec.Cluster.Name,
					schedule.Status.LastScheduleTime, schedule.Status.NextScheduleTime)
			}
		}

		backups := &cnpgv1.BackupList{}
		if err := kubeClient.List(ctx, backups, client.InNamespace(namespace)); err != nil {
			fmt.Fprintf(GinkgoWriter, "backup/restore diagnostics: listing Backups: %v\n", err)
		} else {
			for i := range backups.Items {
				backup := &backups.Items[i]
				fmt.Fprintf(GinkgoWriter, "Backup %s method=%s cluster=%s phase=%s error=%q\n",
					backup.Name, backup.Spec.Method, backup.Spec.Cluster.Name, backup.Status.Phase, backup.Status.Error)
			}
		}

		volumeSnapshots, err := snapshots.SnapshotV1().VolumeSnapshots(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "backup/restore diagnostics: listing VolumeSnapshots: %v\n", err)
			return
		}
		for i := range volumeSnapshots.Items {
			snapshot := &volumeSnapshots.Items[i]
			if snapshot.Status == nil {
				fmt.Fprintf(GinkgoWriter, "VolumeSnapshot %s class=%v status=unavailable\n",
					snapshot.Name, snapshot.Spec.VolumeSnapshotClassName)
				continue
			}
			fmt.Fprintf(GinkgoWriter, "VolumeSnapshot %s class=%v ready=%v boundContent=%v error=%v\n",
				snapshot.Name, snapshot.Spec.VolumeSnapshotClassName, snapshot.Status.ReadyToUse,
				snapshot.Status.BoundVolumeSnapshotContentName, snapshot.Status.Error)
		}
	})
}

func executePostgresSQL(
	ctx context.Context,
	kubeClient client.Client,
	deployment *testenv.Deployment,
	clusterKey types.NamespacedName,
	sql string,
) (string, error) {
	cluster := &enterprisev4.PostgresCluster{}
	if err := kubeClient.Get(ctx, clusterKey, cluster); err != nil {
		return "", fmt.Errorf("getting PostgresCluster primary: %w", err)
	}
	if cluster.Status.CurrentPrimary == nil || *cluster.Status.CurrentPrimary == "" {
		return "", fmt.Errorf("PostgresCluster %s has no current primary", clusterKey)
	}

	command := []string{
		"psql",
		"--username=postgres",
		"--dbname=" + backupRestoreDatabaseName,
		"--no-password",
		"--no-psqlrc",
		"--no-align",
		"--tuples-only",
		"--single-transaction",
		"--set=ON_ERROR_STOP=1",
		"--command", sql,
	}

	var stdout, stderr string
	var execErr error
	pollErr := wait.PollUntilContextTimeout(ctx, testenv.PollInterval, postgresExecRetryTimeout, true, func(attemptCtx context.Context) (bool, error) {
		execCtx, cancel := context.WithTimeout(attemptCtx, postgresExecAttemptTimeout)
		defer cancel()
		stdout, stderr, execErr = deployment.PodExecCommand(execCtx, *cluster.Status.CurrentPrimary, command, "", false)
		if execErr == nil {
			return true, nil
		}
		if strings.TrimSpace(stderr) != "" {
			return false, fmt.Errorf("executing PostgreSQL verification query: %w (stderr: %s)", execErr, strings.TrimSpace(stderr))
		}
		return false, nil
	})
	if pollErr != nil {
		if execErr != nil {
			return "", fmt.Errorf("executing PostgreSQL verification query after retries: %w (stderr: %s)", execErr, strings.TrimSpace(stderr))
		}
		return "", fmt.Errorf("executing PostgreSQL verification query after retries: %w", pollErr)
	}
	return strings.TrimSpace(stdout), nil
}

func snapshotBackupUIDs(
	ctx context.Context,
	kubeClient client.Client,
	namespace, clusterName string,
) (map[types.UID]struct{}, error) {
	backups := &cnpgv1.BackupList{}
	if err := kubeClient.List(ctx, backups, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	result := make(map[types.UID]struct{})
	for i := range backups.Items {
		backup := &backups.Items[i]
		if backup.Spec.Cluster.Name == clusterName && backup.Spec.Method == cnpgv1.BackupMethodVolumeSnapshot {
			result[backup.UID] = struct{}{}
		}
	}
	return result, nil
}

func waitForNewCompletedSnapshotBackup(
	ctx context.Context,
	kubeClient client.Client,
	namespace, clusterName string,
	clusterUID types.UID,
	baseline map[types.UID]struct{},
) cnpgv1.Backup {
	var selected cnpgv1.Backup
	Eventually(func(g Gomega) {
		backups := &cnpgv1.BackupList{}
		g.Expect(kubeClient.List(ctx, backups, client.InNamespace(namespace))).To(Succeed())

		candidates := make([]cnpgv1.Backup, 0)
		for i := range backups.Items {
			backup := &backups.Items[i]
			if backup.Spec.Cluster.Name != clusterName || backup.Spec.Method != cnpgv1.BackupMethodVolumeSnapshot {
				continue
			}
			if _, existed := baseline[backup.UID]; existed {
				continue
			}
			owner := metav1.GetControllerOf(backup)
			if owner == nil || owner.UID != clusterUID {
				continue
			}
			candidates = append(candidates, *backup.DeepCopy())
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].CreationTimestamp.Equal(&candidates[j].CreationTimestamp) {
				return candidates[i].Name < candidates[j].Name
			}
			return candidates[i].CreationTimestamp.Before(&candidates[j].CreationTimestamp)
		})
		g.Expect(candidates).NotTo(BeEmpty(), "waiting for a newly scheduled volumeSnapshot backup")
		if len(candidates) == 0 {
			return
		}

		candidate := candidates[0]
		switch candidate.Status.Phase {
		case cnpgv1.BackupPhaseFailed, cnpgv1.BackupPhaseWalArchivingFailing, cnpgv1.BackupPhaseDefinitionInvalid:
			StopTrying(fmt.Sprintf("CNPG backup %s failed in phase %s: %s", candidate.Name, candidate.Status.Phase, candidate.Status.Error)).Now()
		}
		g.Expect(candidate.Status.Phase).To(Equal(cnpgv1.BackupPhase(cnpgv1.BackupPhaseCompleted)))
		selected = *candidate.DeepCopy()
	}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed())
	return selected
}
