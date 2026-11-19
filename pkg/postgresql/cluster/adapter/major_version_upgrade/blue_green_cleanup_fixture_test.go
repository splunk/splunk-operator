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
	"context"
	"fmt"
	"testing"

	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type cleanupFixture struct {
	client          client.Client
	runtime         *BlueGreenCleanup
	parent          *platformv1alpha1.PostgresCluster
	active, retired *cnpgv1.Cluster
}

func newCleanupFixture(t *testing.T) *cleanupFixture {
	t.Helper()
	scheme, parent, bg, active, retirement := cleanupInventoryFixture(t)
	require.NoError(t, appsv1.AddToScheme(scheme))
	parent.Generation = 3
	parent.Spec.PostgresMajorUpgradeConfig = &platformv1alpha1.PostgresMajorUpgradeConfig{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeConfig{Cleanup: new(true)}}
	owner := []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}
	active.OwnerReferences = owner
	active.Spec.Instances = 1
	active.Status = cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy, Instances: 1, ReadyInstances: 1, CurrentPrimary: "green-1", WriteService: "green-rw", InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"green-1"}}}
	retired := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: retirement.Name, Namespace: parent.Namespace, UID: retirement.UID, OwnerReferences: owner}}
	// Cancelled retires the candidate while the original source stays authoritative.
	bg.Blue, bg.Green = bg.Green, bg.Blue
	now := metav1.Now()
	bg.Gates = &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now, SourceAccessRestoredAt: &now, ReplicationRemovedAt: &now, CredentialsRemovedAt: &now, GreenFencedAt: &now, CompletedAt: &now}}
	parent.Status.ProvisionerRef = bg.Blue.Ref.DeepCopy()
	parent.Status.Resources = &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "canonical"}}
	parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: new("Cancelled"), Strategy: new(mvutypes.MajorUpgradeFlowBlueGreen), SourcePgVersion: new("17"), TargetPgVersion: new("18"), BlueGreen: bg}}
	endpoint, err := pgconninfo.ServiceFQDN(active.Status.WriteService, parent.Namespace)
	require.NoError(t, err)
	canonical := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "canonical", Namespace: parent.Namespace, UID: "canonical-uid", OwnerReferences: owner}, Data: map[string]string{pgconninfo.KeyClusterRWEndpoint: endpoint}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(parent, &platformv1alpha1.PostgresDatabase{}).WithObjects(parent, active, retired, canonical).Build()
	runtime, err := NewBlueGreenCleanup(c, c, client.ObjectKeyFromObject(parent), parent)
	require.NoError(t, err)
	return &cleanupFixture{client: c, runtime: runtime, parent: parent, active: active, retired: retired}
}

// retirementStatus prepares the engine input without running workflow orchestration.
func (f *cleanupFixture) retirementStatus(t *testing.T) *platformv1alpha1.PostgresBlueGreenUpgradeStatus {
	t.Helper()
	bg := f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen
	if bg.Cleanup == nil {
		ref := testResourceReference(t, f.retired)
		bg.Cleanup = &platformv1alpha1.BlueGreenCleanupStatus{EnvironmentRef: &ref}
	}
	protected, err := publishedCleanupSecrets(t.Context(), f.client, f.parent)
	require.NoError(t, err)
	roots, err := cleanupRoots(t.Context(), f.client, f.parent, bg, f.active, bg.Cleanup.EnvironmentRef, protected)
	require.NoError(t, err)
	bg.Cleanup.RetiredResources = roots
	return bg
}

func (f *cleanupFixture) retire(t *testing.T) bool {
	t.Helper()
	bg := f.retirementStatus(t)
	authority := testResourceReference(t, f.active)
	done, err := f.runtime.retireResources(t.Context(), f.parent, bg, f.active, func(ctx context.Context) error {
		parent := &platformv1alpha1.PostgresCluster{}
		if err := f.client.Get(ctx, client.ObjectKeyFromObject(f.parent), parent); err != nil {
			return err
		}
		if parent.UID != f.parent.UID {
			return fmt.Errorf("parent identity changed")
		}
		_, _, err := pgcnpg.ObserveRetirementEnvironments(ctx, f.client, parent, authority, bg.Cleanup.EnvironmentRef)
		return err
	})
	require.NoError(t, err)
	return done
}
