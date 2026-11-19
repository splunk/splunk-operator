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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("PostgresCluster retirement status", Label("postgres"), func() {
	It("persists independent parent and child retirement evidence", func() {
		ctx := context.Background()
		parent := &platformv1alpha1.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "retirement-status-", Namespace: "default"},
			Spec:       platformv1alpha1.PostgresClusterSpec{Class: "retirement-status", Images: pgtestfixture.Images()},
		}
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, parent)).To(Succeed()) })
		child := &platformv1alpha1.PostgresDatabase{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "retirement-child-", Namespace: "default"},
			Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: parent.Name}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "app"}}},
		}
		Expect(k8sClient.Create(ctx, child)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, child)).To(Succeed()) })
		Expect(child.Status.EnvironmentRetirement).To(BeNil())
		now := metav1.NewTime(time.Now().Truncate(time.Second))
		environment := corev1.ObjectReference{APIVersion: "postgresql.cnpg.io/v1", Kind: "Cluster", Namespace: "default", Name: "blue", UID: "environment-uid"}
		resource := corev1.ObjectReference{APIVersion: environment.APIVersion, Kind: "Database", Namespace: "default", Name: "old-app", UID: "provider-uid"}
		cleanup := &platformv1alpha1.BlueGreenCleanupStatus{
			State: platformv1alpha1.BlueGreenCleanupStateCleaning, EnvironmentRef: &environment, RetirementGeneration: new(int64(1)), StartedAt: &now,
			RetiredResources: []corev1.ObjectReference{{APIVersion: environment.APIVersion, Kind: "Pooler", Namespace: environment.Namespace, Name: "old-pooler", UID: "pooler-uid"}},
			RetirementAcknowledgements: []platformv1alpha1.BlueGreenRetirementAcknowledgementStatus{{
				PostgresDatabaseName: child.Name, PostgresDatabaseUID: string(child.UID), PostgresDatabaseGeneration: child.Generation,
				DatabaseName: "app", Resources: []corev1.ObjectReference{resource},
			}},
		}
		parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{
			Phase: new("Completed"), Strategy: new("blueGreen"), SourcePgVersion: new("17"), TargetPgVersion: new("18"),
			BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Cleanup: cleanup, Gates: &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{GreenReplicationRemovedAt: &now}}},
		}}
		Expect(k8sClient.Status().Update(ctx, parent)).To(Succeed())
		child.Status.EnvironmentRetirement = &platformv1alpha1.PostgresDatabaseEnvironmentRetirementStatus{
			ParentUID: parent.UID, AttemptID: "attempt", RetirementGeneration: 1, ObservedDatabaseGeneration: child.Generation, EnvironmentRef: environment,
			Databases: []platformv1alpha1.DatabaseEnvironmentRetirementStatus{{DatabaseName: "app", Resources: []corev1.ObjectReference{resource}, AcknowledgedAt: &now}},
		}
		Expect(k8sClient.Status().Update(ctx, child)).To(Succeed())
		observedParent := &platformv1alpha1.PostgresCluster{}
		observedChild := &platformv1alpha1.PostgresDatabase{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), observedParent)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(child), observedChild)).To(Succeed())
		Expect(observedParent.Status.PostgresMajorUpgradeStatus).To(Equal(parent.Status.PostgresMajorUpgradeStatus))
		Expect(observedChild.Status.EnvironmentRetirement).To(Equal(child.Status.EnvironmentRetirement))
		parentCopy := observedParent.DeepCopy()
		parentCopy.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Gates.Cancellation.GreenReplicationRemovedAt.Time = time.Time{}
		Expect(observedParent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Gates.Cancellation.GreenReplicationRemovedAt).To(Equal(&now))
		parentCopy.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Cleanup.RetirementAcknowledgements[0].Resources[0].UID = "changed"
		Expect(observedParent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Cleanup.RetirementAcknowledgements[0].Resources[0].UID).To(Equal(resource.UID))
		parentCopy.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Cleanup.RetiredResources[0].UID = "changed"
		Expect(observedParent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Cleanup.RetiredResources[0].UID).To(Equal(types.UID("pooler-uid")))
		childCopy := observedChild.DeepCopy()
		childCopy.Status.EnvironmentRetirement.Databases[0].Resources[0].UID = "changed"
		Expect(observedChild.Status.EnvironmentRetirement.Databases[0].Resources[0].UID).To(Equal(resource.UID))
	})
})
