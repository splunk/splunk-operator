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
	"context"
	"fmt"
	"sort"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	snapshotclient "github.com/kubernetes-csi/external-snapshotter/client/v8/clientset/versioned"
	"github.com/splunk/splunk-operator/test/testenv"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// RequireVolumeSnapshotClass verifies the cluster-scoped snapshot contract
// required by PostgreSQL snapshot workflows.
func RequireVolumeSnapshotClass(ctx context.Context, name string) (snapshotclient.Interface, string) {
	GinkgoHelper()
	restConfig, err := config.GetConfig()
	Expect(err).To(Succeed())
	snapshots, err := snapshotclient.NewForConfig(restConfig)
	Expect(err).To(Succeed())
	snapshotClass, err := snapshots.SnapshotV1().VolumeSnapshotClasses().Get(ctx, name, metav1.GetOptions{})
	Expect(err).To(Succeed(), "required VolumeSnapshotClass %q is unavailable", name)
	Expect(snapshotClass.Driver).NotTo(BeEmpty())
	Expect(string(snapshotClass.DeletionPolicy)).To(Equal("Delete"),
		"the E2E VolumeSnapshotClass must delete backing snapshots during cleanup")
	return snapshots, snapshotClass.Driver
}

func SnapshotBackupUIDs(
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

func WaitForNewCompletedSnapshotBackup(
	ctx context.Context,
	kubeClient client.Client,
	namespace, clusterName string,
	clusterUID types.UID,
	baseline map[types.UID]struct{},
) cnpgv1.Backup {
	GinkgoHelper()
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
