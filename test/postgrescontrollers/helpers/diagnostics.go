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
package helpers

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	snapshotclient "github.com/kubernetes-csi/external-snapshotter/client/v8/clientset/versioned"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RegisterSnapshotFailureDump records PostgreSQL and CSI snapshot state when a
// snapshot-dependent spec fails.
func RegisterSnapshotFailureDump(kubeClient client.Client, snapshots snapshotclient.Interface, namespace string) {
	GinkgoHelper()
	DeferCleanup(func(ctx SpecContext) {
		if !CurrentSpecReport().Failed() {
			return
		}

		clusters := &enterprisev4.PostgresClusterList{}
		if err := kubeClient.List(ctx, clusters, client.InNamespace(namespace)); err != nil {
			fmt.Fprintf(GinkgoWriter, "snapshot diagnostics: listing PostgresClusters: %v\n", err)
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
			fmt.Fprintf(GinkgoWriter, "snapshot diagnostics: listing CNPG Clusters: %v\n", err)
		} else {
			for i := range cnpgClusters.Items {
				cluster := &cnpgClusters.Items[i]
				fmt.Fprintf(GinkgoWriter, "CNPG Cluster %s phase=%s instances=%d readyInstances=%d currentPrimary=%s targetPrimary=%s instancesStatus=%v\n",
					cluster.Name, cluster.Status.Phase, cluster.Status.Instances, cluster.Status.ReadyInstances,
					cluster.Status.CurrentPrimary, cluster.Status.TargetPrimary, cluster.Status.InstancesStatus)
			}
		}

		schedules := &cnpgv1.ScheduledBackupList{}
		if err := kubeClient.List(ctx, schedules, client.InNamespace(namespace)); err != nil {
			fmt.Fprintf(GinkgoWriter, "snapshot diagnostics: listing ScheduledBackups: %v\n", err)
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
			fmt.Fprintf(GinkgoWriter, "snapshot diagnostics: listing Backups: %v\n", err)
		} else {
			for i := range backups.Items {
				backup := &backups.Items[i]
				fmt.Fprintf(GinkgoWriter, "Backup %s method=%s cluster=%s phase=%s error=%q\n",
					backup.Name, backup.Spec.Method, backup.Spec.Cluster.Name, backup.Status.Phase, backup.Status.Error)
			}
		}

		volumeSnapshots, err := snapshots.SnapshotV1().VolumeSnapshots(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "snapshot diagnostics: listing VolumeSnapshots: %v\n", err)
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
