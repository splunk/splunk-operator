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

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgtestfixture "github.com/splunk/splunk-operator/internal/testutil/postgres"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("PostgresCluster completion status", Label("postgres"), func() {
	It("persists committed recovery and completion receipts", func() {
		ctx := context.Background()
		cluster := &platformv1alpha1.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "completion-status-", Namespace: "default"},
			Spec:       platformv1alpha1.PostgresClusterSpec{Class: "completion-status", Images: pgtestfixture.Images()},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, cluster)).To(Succeed()) })
		now := metav1.NewTime(time.Now().Truncate(time.Second))
		cluster.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase: new("Completed"), Strategy: new("blueGreen"), SourcePgVersion: new("17"), TargetPgVersion: new("18"),
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
				AttemptID:      "attempt",
				Recoverability: &platformv1alpha1.BlueGreenRecoverabilityStatus{TargetBackupName: new("target-backup"), BackupVerifiedAt: &now, WALArchiveBaselineVerifiedAt: &now},
				Completion:     &platformv1alpha1.BlueGreenCompletionStatus{StartedAt: &now, SourceStoppedAt: &now, ReplicationRemovedAt: &now, SourceCredentialsRetiredAt: &now, CredentialsRemovedAt: &now},
			},
		}}
		Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())
		observed := &platformv1alpha1.PostgresCluster{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), observed)).To(Succeed())
		Expect(observed.Status.PostgresMajorUpgradeStatus).To(Equal(cluster.Status.PostgresMajorUpgradeStatus))
		copy := observed.DeepCopy()
		copy.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Completion.SourceStoppedAt.Time = now.Add(time.Second)
		Expect(observed.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Completion.SourceStoppedAt).To(Equal(&now))
	})
})
