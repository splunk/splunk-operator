/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package core

import (
	"testing"

	pgtestfixture "github.com/splunk/splunk-operator/internal/testutil/postgres"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func availableTargetFixture() (*platformv1alpha1.PostgresCluster, *cnpgv1.Cluster, *reconcileContracts) {
	parent := &platformv1alpha1.PostgresCluster{Name: "orders", Namespace: "default", UID: "parent-uid", Spec: platformv1alpha1.PostgresClusterSpec{Images: pgtestfixture.Images()}}
	green := &cnpgv1.Cluster{Name: "orders-green", Namespace: parent.Namespace, UID: "green-uid", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}, Spec: cnpgv1.ClusterSpec{ImageName: "candidate-image"}}
	ref := &corev1.ObjectReference{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Name: green.Name, Namespace: green.Namespace, UID: green.UID}
	parent.Status.ProvisionerRef = ref.DeepCopy()
	parent.Status.Resources = &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "access"}}
	parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: *ref}, Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{TargetAvailableAt: new(metav1.Now())}, CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: new(metav1.Now()), Target: ref.DeepCopy(), CanonicalConfigMap: &corev1.ObjectReference{Name: "access", UID: "canonical-uid"}}}}}
	contracts := &reconcileContracts{Authority: identitytypes.ClusterCard{Authoritative: identitytypes.Environment{Identity: identitytypes.ObjectIdentity{APIVersion: ref.APIVersion, Kind: ref.Kind, Namespace: ref.Namespace, Name: ref.Name, UID: ref.UID}}}}
	return parent, green, contracts
}

func availableTargetReader(t *testing.T, objects ...client.Object) client.Reader {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	objects = append(objects, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-green-superuser", Namespace: "default"}, Data: map[string][]byte{secretKeyPassword: []byte("test-password")}})
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestAvailableUpgradeTargetPublishesObservedGreenContract(t *testing.T) {
	parent, green, contracts := availableTargetFixture()
	err := observeAvailableUpgradeTarget(t.Context(), availableTargetReader(t, parent, green), parent, contracts, "attempt")
	require.NoError(t, err)
	require.NotNil(t, contracts.CNPGCluster)
	assert.Equal(t, green.UID, contracts.CNPGCluster.UID)
	assert.Equal(t, green.Spec, contracts.CNPGCluster.Spec)
	require.NotNil(t, contracts.Secret)
	assert.Equal(t, "orders-green-superuser", contracts.Secret.Name)
	assert.False(t, contracts.TLSBackend.observed, "observing the target must not publish an unverified TLS contract")
}

func TestAvailableUpgradeTargetRejectsStaleOrForeignEnvironment(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*platformv1alpha1.PostgresCluster, *cnpgv1.Cluster, *reconcileContracts)
	}{
		{"replacement UID", func(_ *platformv1alpha1.PostgresCluster, g *cnpgv1.Cluster, _ *reconcileContracts) {
			g.UID = "replacement"
		}},
		{"different name", func(_ *platformv1alpha1.PostgresCluster, g *cnpgv1.Cluster, _ *reconcileContracts) {
			g.Name = "replacement"
		}},
		{"foreign controller", func(_ *platformv1alpha1.PostgresCluster, g *cnpgv1.Cluster, _ *reconcileContracts) {
			g.OwnerReferences[0].UID = "foreign"
		}},
		{"wrong owner kind", func(_ *platformv1alpha1.PostgresCluster, g *cnpgv1.Cluster, _ *reconcileContracts) {
			g.OwnerReferences[0].Kind = "Other"
		}},
		{"deleting target", func(_ *platformv1alpha1.PostgresCluster, g *cnpgv1.Cluster, _ *reconcileContracts) {
			g.DeletionTimestamp = new(metav1.Now())
			g.Finalizers = []string{"retained"}
		}},
		{"changed provisioner", func(p *platformv1alpha1.PostgresCluster, _ *cnpgv1.Cluster, _ *reconcileContracts) {
			p.Status.ProvisionerRef.UID = "other"
		}},
		{"wrong resolved authority", func(_ *platformv1alpha1.PostgresCluster, _ *cnpgv1.Cluster, c *reconcileContracts) {
			c.Authority.Authoritative.Identity.UID = "other"
		}},
		{"removed receipt", func(p *platformv1alpha1.PostgresCluster, _ *cnpgv1.Cluster, _ *reconcileContracts) {
			p.Status.PostgresMajorUpgradeStatus[0].BlueGreen.CommitReceipt = nil
		}},
		{"changed attempt", func(p *platformv1alpha1.PostgresCluster, _ *cnpgv1.Cluster, _ *reconcileContracts) {
			p.Status.PostgresMajorUpgradeStatus[0].BlueGreen.AttemptID = "another"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent, green, contracts := availableTargetFixture()
			cached := parent.DeepCopy()
			test.change(parent, green, contracts)
			err := observeAvailableUpgradeTarget(t.Context(), availableTargetReader(t, parent, green), parent, contracts, cached.Status.PostgresMajorUpgradeStatus[0].BlueGreen.AttemptID)
			require.Error(t, err)
			assert.Nil(t, contracts.CNPGCluster)
		})
	}
}

func TestAvailableUpgradeTargetRequiresUncachedReaderOnlyAtAvailability(t *testing.T) {
	parent, _, contracts := availableTargetFixture()
	assert.Error(t, observeAvailableUpgradeTarget(t.Context(), nil, parent, contracts, "attempt"))
	parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Switchover.TargetAvailableAt = nil
	assert.NoError(t, observeAvailableUpgradeTarget(t.Context(), nil, parent, contracts, "attempt"))
}
