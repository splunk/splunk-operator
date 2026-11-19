/*
Copyright 2026.

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

package majorupgradeadapter

import (
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func readyBarmanInstances(t *testing.T, f *cutoverFixture) (*cnpgv1.Cluster, []*corev1.Pod) {
	t.Helper()
	green := &cnpgv1.Cluster{}
	require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKey{Namespace: f.runtime.key.Namespace, Name: "green"}, green))
	green.Spec.Instances = 2
	green.Status.Instances, green.Status.ReadyInstances = 2, 2
	green.Status.CurrentPrimary, green.Status.TargetPrimary = "green-1", "green-1"
	green.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"green-1", "green-2"}}
	require.NoError(t, f.runtime.client.Update(t.Context(), green))
	pods := []*corev1.Pod{
		observedInstanceTestPod(green, "green-1", "10.42.0.17"),
		observedInstanceTestPod(green, "green-2", "10.42.0.18"),
	}
	for i, pod := range pods {
		container := corev1.Container{Name: "plugin-barman-cloud", Image: "barman:test"}
		status := corev1.ContainerStatus{Name: container.Name, Ready: true, Started: new(true), State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}
		if i == 0 {
			pod.Spec.Containers = []corev1.Container{container}
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{status}
		} else {
			container.RestartPolicy = new(corev1.ContainerRestartPolicyAlways)
			pod.Spec.InitContainers = []corev1.Container{container}
			pod.Status.InitContainerStatuses = []corev1.ContainerStatus{status}
		}
		require.NoError(t, f.runtime.client.Create(t.Context(), pod))
	}
	return green, pods
}

func TestCompletionWaitsForBarmanRolloutBeforeBackupsAndSchedules(t *testing.T) {
	for _, condition := range []string{"missing primary plugin", "unready native sidecar", "archive restart pending", "primary changing"} {
		t.Run(condition, func(t *testing.T) {
			f, c, sql := completionFixture(t)
			c.config.CNPG.Backup.BarmanObjectStore = &platformv1alpha1.CNPGBarmanObjectStoreConfig{DestinationPath: "s3://fixture/green"}
			green, pods := readyBarmanInstances(t, f)
			readyPods := []*corev1.Pod{pods[0].DeepCopy(), pods[1].DeepCopy()}
			switch condition {
			case "missing primary plugin":
				pods[0].Spec.Containers, pods[0].Status.ContainerStatuses = nil, nil
			case "unready native sidecar":
				pods[1].Status.InitContainerStatuses[0].Ready = false
			case "archive restart pending":
				sql.archivingReady = false
			case "primary changing":
				green.Status.TargetPrimary = "green-2"
				require.NoError(t, f.runtime.client.Update(t.Context(), green))
			}
			for _, pod := range pods {
				status := pod.Status.DeepCopy()
				require.NoError(t, f.runtime.client.Update(t.Context(), pod))
				pod.Status = *status
				require.NoError(t, f.runtime.client.Status().Update(t.Context(), pod))
			}
			for range 8 {
				completionStep(t, f, c)
			}
			backups, schedules := &cnpgv1.BackupList{}, &cnpgv1.ScheduledBackupList{}
			require.NoError(t, f.runtime.reader.List(t.Context(), backups))
			require.NoError(t, f.runtime.reader.List(t.Context(), schedules))
			assert.Empty(t, backups.Items, "a configured plugin is not proof the target can back up")
			assert.Empty(t, schedules.Items, "scheduled backups must use the same readiness gate")
			assert.Nil(t, f.status().Recoverability)
			assert.Zero(t, sql.greenCalls)
			assert.NotNil(t, f.status().CommitReceipt)
			require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKeyFromObject(green), green))
			require.Len(t, green.Spec.Plugins, 1)
			assert.Equal(t, condition == "archive restart pending", *green.Spec.Plugins[0].IsWALArchiver,
				"WAL archiving must stay disabled while any sidecar is missing, unready, or the primary is changing")
			for i, pod := range pods {
				require.NoError(t, f.runtime.client.Get(t.Context(), client.ObjectKeyFromObject(pod), pod))
				pod.Spec, pod.Status = readyPods[i].Spec, readyPods[i].Status
				status := pod.Status.DeepCopy()
				require.NoError(t, f.runtime.client.Update(t.Context(), pod))
				pod.Status = *status
				require.NoError(t, f.runtime.client.Status().Update(t.Context(), pod))
			}
			require.NoError(t, f.runtime.client.Get(t.Context(), client.ObjectKeyFromObject(green), green))
			green.Status.TargetPrimary = green.Status.CurrentPrimary
			require.NoError(t, f.runtime.client.Update(t.Context(), green))
			sql.archivingReady = true
			backup := waitForTargetBackup(t, f, c)
			assert.Contains(t, sql.archivingHosts, "10.42.0.17")
			assert.Contains(t, sql.archivingHosts, "10.42.0.18", "a standby may be promoted when the backup starts")
			completionStep(t, f, c)
			require.NoError(t, f.runtime.reader.List(t.Context(), backups))
			require.Len(t, backups.Items, 1)
			assert.Equal(t, backup.UID, backups.Items[0].UID)
		})
	}
}

func TestCompletionStillWaitsForPublicationApply(t *testing.T) {
	f, c, sql := completionFixture(t)
	backup := waitForTargetBackup(t, f, c)
	completeTargetBackup(t, f, backup)
	sql.archived = true
	completionStep(t, f, c)
	bg := f.status()
	db := bg.DatabaseReplication[0].DatabaseName
	publication := &cnpgv1.Publication{Name: "pending-publication", Namespace: f.runtime.key.Namespace, UID: "publication-uid", Generation: 2,
		Labels:          map[string]string{blueGreenAttemptLabel: bg.AttemptID},
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(f.runtime.owner, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))},
		Finalizers:      []string{cnpgutils.PublicationFinalizerName},
		Spec:            desiredPublicationSpec(bg.Blue.Ref.Name, db, publicationName(f.runtime.key.Name, db)),
	}
	publication.Spec.ReclaimPolicy = cnpgv1.PublicationReclaimRetain
	publication.Status.ObservedGeneration, publication.Status.Applied = 1, new(false)
	require.NoError(t, f.runtime.client.Create(t.Context(), publication))
	completionStep(t, f, c)
	require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKeyFromObject(publication), publication))
	assert.Nil(t, publication.DeletionTimestamp, "Publication writers keep their successful-apply barrier")
	assert.Zero(t, sql.greenCalls)
	publication.Status.ObservedGeneration, publication.Status.Applied = publication.Generation, new(true)
	require.NoError(t, f.runtime.client.Update(t.Context(), publication))
	completionStep(t, f, c)
	require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKeyFromObject(publication), publication))
	assert.NotNil(t, publication.DeletionTimestamp)
}

func TestSnapshotCompletionDoesNotRequireBarmanSidecars(t *testing.T) {
	f, c, sql := completionFixture(t)
	sql.archivingReady = false
	backup := waitForTargetBackup(t, f, c)
	assert.Equal(t, cnpgv1.BackupMethodVolumeSnapshot, backup.Spec.Method)
	assert.Nil(t, backup.Spec.PluginConfiguration)
	assert.Empty(t, sql.archivingHosts, "snapshot requests bypass the Barman rollout gate")
	completeTargetBackup(t, f, backup)
	for range 3 {
		completionStep(t, f, c)
	}
	assert.Nil(t, f.status().Recoverability, "a snapshot does not bypass the separate backup/WAL recovery verifier")
	assert.Zero(t, sql.greenCalls, "replication teardown still requires recovery evidence")
}

func TestCompletionRetiresFailedSubscriptionOnlyAfterWriterQuiescence(t *testing.T) {
	f, c, sql := completionFixture(t)
	backup := waitForTargetBackup(t, f, c)
	completeTargetBackup(t, f, backup)
	sql.archived = true
	completionStep(t, f, c)
	bg := f.status()
	db := bg.DatabaseReplication[0].DatabaseName
	sub := &cnpgv1.Subscription{Name: "failed-subscription", Namespace: f.runtime.key.Namespace, UID: "subscription-uid", Generation: 2,
		Labels:          map[string]string{blueGreenAttemptLabel: bg.AttemptID},
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(f.runtime.owner, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))},
		Finalizers:      []string{cnpgutils.SubscriptionFinalizerName, "example.com/hold"},
		Spec: cnpgv1.SubscriptionSpec{ClusterRef: corev1.LocalObjectReference{Name: bg.Green.Ref.Name}, DBName: db,
			Name: subscriptionName(f.runtime.key.Name, db), ReclaimPolicy: cnpgv1.SubscriptionReclaimDelete,
			Parameters: map[string]string{"enabled": "true", "failover": "true", "slot_name": replicationSlotName(f.runtime.key.Name, db)}},
	}
	sub.Status.ObservedGeneration, sub.Status.Applied = 1, new(false)
	sub.Status.Message = "cannot set option failover for enabled subscription (SQLSTATE 55000)"
	require.NoError(t, f.runtime.client.Create(t.Context(), sub))
	completionStep(t, f, c)
	require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKeyFromObject(sub), sub))
	assert.Equal(t, cnpgv1.SubscriptionReclaimRetain, sub.Spec.ReclaimPolicy)
	assert.Nil(t, sub.DeletionTimestamp, "persist Retain before requesting deletion")
	completionStep(t, f, c)
	require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKeyFromObject(sub), sub))
	require.NotNil(t, sub.DeletionTimestamp, "failed apply cannot prevent retirement of retained intent")
	completionStep(t, f, c)
	assert.Zero(t, sql.greenCalls, "SQL must wait for CNPG to stop applying the declaration")
	sub.Finalizers = []string{"example.com/hold"}
	require.NoError(t, f.runtime.client.Update(t.Context(), sub))
	completionStep(t, f, c)
	assert.Zero(t, sql.greenCalls, "preserve other controllers' finalizers")
	sub.Finalizers = nil
	require.NoError(t, f.runtime.client.Update(t.Context(), sub))
	completionStep(t, f, c)
	assert.Equal(t, 1, sql.greenCalls)
	assert.NotNil(t, f.status().Completion.ReplicationRemovedAt)
	assert.NotNil(t, f.status().Recoverability)
	assert.NotContains(t, f.calls, "restore-blue")
}
