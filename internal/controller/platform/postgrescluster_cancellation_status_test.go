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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgtestfixture "github.com/splunk/splunk-operator/internal/testutil/postgres"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("PostgresCluster cancellation SQL receipt", Label("postgres"), func() {
	It("persists the optional receipt and copies it independently", func() {
		ctx := context.Background()
		parent := &platformv1alpha1.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "cancellation-receipt-", Namespace: "default"},
			Spec:       platformv1alpha1.PostgresClusterSpec{Class: "cancellation-receipt", Images: pgtestfixture.Images()},
		}
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, parent)).To(Succeed()) })
		now := metav1.NewTime(time.Now().Truncate(time.Second))
		parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase: new("Cancelled"), Strategy: new("blueGreen"),
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", SubscriptionCreationAuthorized: new(false), Gates: &platformv1alpha1.BlueGreenGateStatus{
				Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now, GreenReplicationRemovedAt: &now, GreenReplicationProofVersion: 1},
			}},
		}}
		Expect(k8sClient.Status().Update(ctx, parent)).To(Succeed())
		observed := &platformv1alpha1.PostgresCluster{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), observed)).To(Succeed())
		Expect(observed.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Gates.Cancellation.GreenReplicationRemovedAt).To(Equal(&now))
		Expect(observed.Status.PostgresMajorUpgradeStatus[0].BlueGreen.SubscriptionCreationAuthorized).To(Equal(new(false)))
		Expect(observed.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Gates.Cancellation.GreenReplicationProofVersion).To(Equal(int32(1)))
		copy := observed.DeepCopy()
		copy.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Gates.Cancellation.GreenReplicationRemovedAt.Time = time.Time{}
		Expect(observed.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Gates.Cancellation.GreenReplicationRemovedAt).To(Equal(&now))
	})
	It("allows a deleting retained subscription to wake CNPG finalization", func() {
		ctx := context.Background()
		subscription := &cnpgv1.Subscription{ObjectMeta: metav1.ObjectMeta{GenerateName: "quiesce-", Namespace: "default", Finalizers: []string{cnpgutils.SubscriptionFinalizerName}},
			Spec: cnpgv1.SubscriptionSpec{ClusterRef: corev1.LocalObjectReference{Name: "green"}, Name: "test", DBName: "postgres", ExternalClusterName: "source", ReclaimPolicy: cnpgv1.SubscriptionReclaimRetain, Parameters: map[string]string{"enabled": "true"}}}
		Expect(k8sClient.Create(ctx, subscription)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(subscription), subscription)).To(Succeed())
			subscription.Finalizers = nil
			Expect(k8sClient.Update(ctx, subscription)).To(Succeed())
		})
		Expect(k8sClient.Delete(ctx, subscription)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(subscription), subscription)).To(Succeed())
		generation := subscription.Generation
		subscription.Spec.Parameters["enabled"] = "false"
		Expect(k8sClient.Update(ctx, subscription)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(subscription), subscription)).To(Succeed())
		Expect(subscription.Generation).To(Equal(generation + 1))
		Expect(subscription.DeletionTimestamp).NotTo(BeNil())
		Expect(subscription.Finalizers).To(ContainElement(cnpgutils.SubscriptionFinalizerName))
	})
})
