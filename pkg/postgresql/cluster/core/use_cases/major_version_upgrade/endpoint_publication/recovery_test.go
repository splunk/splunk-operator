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

package endpointpublication

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func preparedRecoveryFixture(t *testing.T) (*Coordinator, *memoryStore, types.NamespacedName, Proof) {
	t.Helper()
	coordinator, store, key, proof := fixture(t)
	now := metav1.Now()
	bg := currentBG(store)
	bg.Gates = &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: 1, ConsumedAt: now.DeepCopy()}}
	bg.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{
		Sequence: 1, StartedAt: now.DeepCopy(), DeadlineAt: new(metav1.NewTime(time.Now().Add(time.Hour))), LatchedTimeout: &metav1.Duration{Duration: time.Hour},
		ControlIdentity: &platformv1alpha1.BlueGreenControlIdentityStatus{RoleName: "operator-control", PreparedAt: now.DeepCopy(), SecretRef: corev1.ObjectReference{Name: "operator-control", UID: "control-uid"}},
		FenceAppliedAt:  now.DeepCopy(), FenceVerifiedAt: now.DeepCopy(), DrainCompletedAt: now.DeepCopy(), FinalBlueLSN: new("0/100"),
		SequenceSynchronization:  &platformv1alpha1.BlueGreenSequenceSynchronizationStatus{CompletedAt: now.DeepCopy(), VerifiedAt: now.DeepCopy()},
		CandidateGuardsRemovedAt: now.DeepCopy(), FinalParityVerifiedAt: now.DeepCopy(),
	}
	store.configMaps["cluster-config"].Data = maps.Clone(proof.Blue)
	store.configMaps["cluster-config"].Data["SUPER_USER_SECRET_REF"] = "blue-superuser"
	store.configMaps["cluster-config"].Data["SERVER_CA_SECRET_REF"] = "blue-ca"
	proof.GreenAccess = map[string]string{"SUPER_USER_NAME": "postgres", "SUPER_USER_SECRET_REF": "green-superuser", "SERVER_CA_SECRET_REF": "green-ca"}
	coordinator.proof = fixedProof{proof: proof}
	require.NoError(t, coordinator.PublishPreparationIntent(t.Context(), key, "attempt"))
	fingerprint := pgconninfo.FingerprintEndpointData(proof.Green)
	store.children[0].Status.EndpointPreparation = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
		AttemptID: "attempt", IntentGeneration: 1, ObservedDatabaseGeneration: 1, GreenClusterUID: "green-uid",
		Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, AcknowledgedAt: new(metav1.Now())}},
	}
	complete, err := coordinator.PreparationComplete(t.Context(), key, "attempt")
	require.NoError(t, err)
	require.True(t, complete)
	return coordinator, store, key, proof
}

func committedRecoveryFixture(t *testing.T) (*Coordinator, *memoryStore, types.NamespacedName, Proof) {
	t.Helper()
	coordinator, store, key, proof := preparedRecoveryFixture(t)
	require.NoError(t, coordinator.CommitCanonical(t.Context(), key, "attempt"))
	require.NotNil(t, currentBG(store).CommitReceipt)
	return coordinator, store, key, proof
}

func TestCommitReceiptRepairsFromCanonicalWithoutLiveGreenOrChildInventory(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*memoryStore)
	}{
		{"deleted child", func(store *memoryStore) { store.children = nil }},
		{"replaced child", func(store *memoryStore) { store.children[0].UID = "replacement-child" }},
		{"changed declaration", func(store *memoryStore) { store.children[0].Spec.Databases[0].Name = "different-database" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			coordinator, store, key, proof := committedRecoveryFixture(t)
			canonical := store.configMaps["cluster-config"].DeepCopy()
			currentBG(store).CommitReceipt = nil
			currentBG(store).Switchover.EndpointCommittedAt = nil
			store.parent.Status.ProvisionerRef = currentBG(store).Blue.Ref.DeepCopy()
			test.mutate(store)
			coordinator.proof = fixedProof{proof: proof, failGreen: true, failBlue: true}
			committed, err := coordinator.ObserveCommit(t.Context(), key, "attempt")
			require.NoError(t, err)
			require.True(t, committed)
			require.NoError(t, coordinator.CommitCanonical(t.Context(), key, "attempt"))
			assert.Equal(t, canonical, store.configMaps["cluster-config"], "receipt repair must not rewrite the external commit record")
			assert.Equal(t, canonical.UID, currentBG(store).CommitReceipt.CanonicalConfigMap.UID)
			assert.Equal(t, currentBG(store).Green.Ref, *store.parent.Status.ProvisionerRef)
			assert.NotNil(t, currentBG(store).Switchover.EndpointCommittedAt)
		})
	}
}

type blueRecoveryProof struct {
	fixedProof
	metadata map[string]string
}

func (p blueRecoveryProof) ResolveBlueAccess(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (map[string]string, error) {
	return maps.Clone(p.metadata), nil
}

func TestMissingCanonicalWithoutReceiptCanOnlyRestoreBlue(t *testing.T) {
	coordinator, store, key, proof := preparedRecoveryFixture(t)
	delete(store.configMaps, "cluster-config")
	coordinator.proof = blueRecoveryProof{fixedProof: fixedProof{proof: proof, failGreen: true}, metadata: map[string]string{"SUPER_USER_SECRET_REF": "blue-superuser", "SERVER_CA_SECRET_REF": "blue-ca"}}
	committed, err := coordinator.ObserveCommit(t.Context(), key, "attempt")
	require.NoError(t, err)
	assert.False(t, committed)
	require.Error(t, coordinator.CommitCanonical(t.Context(), key, "attempt"))
	assert.Nil(t, store.configMaps["cluster-config"])
	require.ErrorIs(t, coordinator.RecoverCanonical(t.Context(), key, "attempt"), ErrCanonicalMismatch)
	assert.Nil(t, store.configMaps["cluster-config"])
	require.NoError(t, coordinator.RestoreUncommittedCanonical(t.Context(), key, "attempt"))
	canonical := store.configMaps["cluster-config"]
	require.NotNil(t, canonical)
	assert.True(t, endpointDataMatches(canonical.Data, proof.Blue))
	assert.Equal(t, "blue", canonical.Annotations[pgconninfo.AnnotationActiveEnvironment])
	assert.Equal(t, "blue-superuser", canonical.Data["SUPER_USER_SECRET_REF"])
	assert.Equal(t, "blue-ca", canonical.Data["SERVER_CA_SECRET_REF"])
	assert.Nil(t, currentBG(store).CommitReceipt)
	assert.Nil(t, currentBG(store).CanonicalRecovery)
}

func TestMissingCommittedCanonicalRecreatesAuthorizedProjectionWithoutChangingReceipt(t *testing.T) {
	coordinator, store, key, proof := committedRecoveryFixture(t)
	receipt := currentBG(store).CommitReceipt.DeepCopy()
	store.parent.Status.ProvisionerRef = currentBG(store).Blue.Ref.DeepCopy()
	delete(store.configMaps, "cluster-config")
	store.children = nil
	coordinator.proof = fixedProof{proof: proof, failGreen: true, failBlue: true}
	committed, err := coordinator.ObserveCommit(t.Context(), key, "attempt")
	require.NoError(t, err)
	require.True(t, committed)
	require.NoError(t, coordinator.RecoverCanonical(t.Context(), key, "attempt"))
	bg := currentBG(store)
	canonical := store.configMaps["cluster-config"]
	assert.Equal(t, receipt, bg.CommitReceipt, "recovery must preserve the historical irreversible receipt")
	assert.Equal(t, bg.Green.Ref, *store.parent.Status.ProvisionerRef, "durable commit evidence repairs the authoritative projection")
	require.NotNil(t, bg.CanonicalRecovery)
	assert.NotEqual(t, receipt.CanonicalConfigMap.UID, canonical.UID)
	assert.Equal(t, canonical.UID, bg.CanonicalRecovery.ConfigMap.UID)
	assert.Equal(t, receipt.Sequence, bg.CanonicalRecovery.CommitSequence)
	assert.NotNil(t, bg.CanonicalRecovery.RecoveredAt)
	assert.Equal(t, "green-superuser", canonical.Data["SUPER_USER_SECRET_REF"])
	assert.Equal(t, "green-ca", canonical.Data["SERVER_CA_SECRET_REF"])
	assert.True(t, endpointDataMatches(canonical.Data, proof.Green))
	assert.True(t, metav1.IsControlledBy(canonical, store.parent))
	_, valid := pgendpointprotocol.CommittedCanonicalRouting(store.parent, bg, canonical, bg.EndpointPreparation.Generation, *bg.EndpointPreparation.EndpointFingerprint)
	assert.True(t, valid)
	before := canonical.DeepCopy()
	require.NoError(t, coordinator.RecoverCanonical(t.Context(), key, "attempt"))
	assert.Equal(t, before, store.configMaps["cluster-config"])
	assert.Equal(t, receipt, currentBG(store).CommitReceipt)
}

func acknowledgePublishedChild(store *memoryStore, proof Proof) {
	fingerprint := pgconninfo.FingerprintEndpointData(proof.Green)
	store.children[0].Status.EndpointPublication = &platformv1alpha1.PostgresDatabaseEndpointAcknowledgementStatus{
		AttemptID: "attempt", IntentGeneration: 1, ObservedDatabaseGeneration: 1, GreenClusterUID: "green-uid",
		Databases: []platformv1alpha1.DatabaseEndpointAcknowledgementStatus{{DatabaseName: "orders", EndpointFingerprint: fingerprint, ConfigMapUID: "db-cm-uid", AcknowledgedAt: new(metav1.Now())}},
	}
	cm := store.configMaps["tenant-orders-config"]
	cm.Data = maps.Clone(proof.Green)
	cm.Annotations = map[string]string{pgconninfo.AnnotationActiveEnvironment: "green", pgconninfo.AnnotationEndpointAttemptID: "attempt", pgconninfo.AnnotationEndpointIntentGeneration: "1", pgconninfo.AnnotationEndpointFingerprint: fingerprint}
}

func TestCanonicalReplacementBlocksPublicationUntilRecoveryIsDurable(t *testing.T) {
	coordinator, store, key, proof := committedRecoveryFixture(t)
	require.NoError(t, coordinator.PublishPublicationIntent(t.Context(), key, "attempt"))
	acknowledgePublishedChild(store, proof)
	complete, err := coordinator.ReconcilePublicationStatus(t.Context(), key, "attempt")
	require.NoError(t, err)
	require.True(t, complete)
	canonical := store.configMaps["cluster-config"]
	canonical.UID = "owned-replacement"
	complete, err = coordinator.ReconcilePublicationStatus(t.Context(), key, "attempt")
	require.NoError(t, err)
	assert.False(t, complete, "matching endpoint data cannot authorize a replacement UID")
	assert.Nil(t, currentBG(store).CanonicalRecovery)
	require.NoError(t, coordinator.RecoverCanonical(t.Context(), key, "attempt"))
	complete, err = coordinator.ReconcilePublicationStatus(t.Context(), key, "attempt")
	require.NoError(t, err)
	assert.True(t, complete)
	assert.Equal(t, "owned-replacement", string(currentBG(store).CanonicalRecovery.ConfigMap.UID))
}

func TestUnownedCanonicalCannotBeRepairedOrPublished(t *testing.T) {
	for _, receipt := range []bool{false, true} {
		t.Run(map[bool]string{false: "before receipt", true: "after receipt"}[receipt], func(t *testing.T) {
			coordinator, store, key, _ := committedRecoveryFixture(t)
			if !receipt {
				currentBG(store).CommitReceipt = nil
			}
			canonical := store.configMaps["cluster-config"]
			canonical.OwnerReferences = nil
			before := canonical.DeepCopy()
			if receipt {
				require.ErrorIs(t, coordinator.RecoverCanonical(t.Context(), key, "attempt"), ErrCanonicalMismatch)
			} else {
				_, err := coordinator.ObserveCommit(t.Context(), key, "attempt")
				require.ErrorIs(t, err, ErrCanonicalMismatch)
				require.ErrorIs(t, coordinator.CommitCanonical(t.Context(), key, "attempt"), ErrCanonicalMismatch)
			}
			assert.Equal(t, before, store.configMaps["cluster-config"])
			assert.Nil(t, currentBG(store).CanonicalRecovery)
		})
	}
}

func TestExpiredDeadlineBlocksNewCommitAndStillRepairsExistingCommit(t *testing.T) {
	coordinator, store, key, proof := preparedRecoveryFixture(t)
	currentBG(store).Switchover.DeadlineAt = new(metav1.NewTime(time.Now().Add(-time.Minute)))
	require.ErrorIs(t, coordinator.CommitCanonical(t.Context(), key, "attempt"), context.DeadlineExceeded)
	assert.True(t, endpointDataMatches(store.configMaps["cluster-config"].Data, proof.Blue))
	assert.Nil(t, currentBG(store).CommitReceipt)
	applyPreparedRouting(store.configMaps["cluster-config"], currentBG(store))
	coordinator.proof = fixedProof{proof: proof, failBlue: true, failGreen: true}
	require.NoError(t, coordinator.CommitCanonical(t.Context(), key, "attempt"))
	assert.NotNil(t, currentBG(store).CommitReceipt)
	delete(store.configMaps, "cluster-config")
	require.NoError(t, coordinator.RecoverCanonical(t.Context(), key, "attempt"))
	assert.NotNil(t, currentBG(store).CanonicalRecovery)
}

type recoveryFailureStore struct {
	*memoryStore
	updateConflict, updateLostResponse, createLostResponse, statusLostResponse bool
}

func (s *recoveryFailureStore) UpdateConfigMap(ctx context.Context, cm *corev1.ConfigMap) error {
	if s.updateConflict {
		s.updateConflict = false
		return apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, cm.Name, errors.New("injected ConfigMap conflict"))
	}
	if err := s.memoryStore.UpdateConfigMap(ctx, cm); err != nil {
		return err
	}
	if s.updateLostResponse {
		s.updateLostResponse = false
		return errors.New("canonical update response lost")
	}
	return nil
}

func (s *recoveryFailureStore) CreateConfigMap(ctx context.Context, cm *corev1.ConfigMap) error {
	if err := s.memoryStore.CreateConfigMap(ctx, cm); err != nil {
		return err
	}
	if s.createLostResponse {
		s.createLostResponse = false
		return errors.New("canonical create response lost")
	}
	return nil
}

func (s *recoveryFailureStore) WriteParentStatus(ctx context.Context, parent *platformv1alpha1.PostgresCluster) error {
	if err := s.memoryStore.WriteParentStatus(ctx, parent); err != nil {
		return err
	}
	if s.statusLostResponse {
		s.statusLostResponse = false
		return errors.New("status response lost")
	}
	return nil
}

func TestCanonicalCommitRecoversConflictsAndLostResponses(t *testing.T) {
	for _, test := range []struct {
		name       string
		setup      func(*recoveryFailureStore)
		firstError bool
	}{
		{"ConfigMap conflict", func(store *recoveryFailureStore) { store.updateConflict = true }, false},
		{"ConfigMap response lost", func(store *recoveryFailureStore) { store.updateLostResponse = true }, false},
		{"status conflict", func(store *recoveryFailureStore) { store.conflicts = 1 }, false},
		{"status response lost", func(store *recoveryFailureStore) { store.statusLostResponse = true }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			coordinator, store, key, proof := preparedRecoveryFixture(t)
			faults := &recoveryFailureStore{memoryStore: store}
			test.setup(faults)
			coordinator.store = faults
			err := coordinator.CommitCanonical(t.Context(), key, "attempt")
			if test.firstError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.True(t, endpointDataMatches(store.configMaps["cluster-config"].Data, proof.Green))
			coordinator.proof = fixedProof{proof: proof, failBlue: true, failGreen: true}
			require.NoError(t, coordinator.CommitCanonical(t.Context(), key, "attempt"))
			assert.NotNil(t, currentBG(store).CommitReceipt)
			assert.Equal(t, int64(1), currentBG(store).CommitReceipt.Sequence)
		})
	}
}

func TestCanonicalRecoverySurvivesCreateResponseLossAndStatusConflict(t *testing.T) {
	coordinator, store, key, proof := committedRecoveryFixture(t)
	receipt := currentBG(store).CommitReceipt.DeepCopy()
	delete(store.configMaps, "cluster-config")
	faults := &recoveryFailureStore{memoryStore: store, createLostResponse: true}
	coordinator.store = faults
	coordinator.proof = fixedProof{proof: proof, failBlue: true, failGreen: true}
	require.ErrorContains(t, coordinator.RecoverCanonical(t.Context(), key, "attempt"), "response lost")
	require.NotNil(t, store.configMaps["cluster-config"], "creation committed despite losing its response")
	assert.Nil(t, currentBG(store).CanonicalRecovery, "a created projection alone cannot satisfy the publication proof")
	store.conflicts = 1
	require.NoError(t, coordinator.RecoverCanonical(t.Context(), key, "attempt"))
	assert.NotNil(t, currentBG(store).CanonicalRecovery)
	assert.Equal(t, receipt, currentBG(store).CommitReceipt)
}

func TestCommittedCanonicalRepairsPreparedRoutingAndAccessSelectors(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*corev1.ConfigMap)
	}{
		{"routing", func(cm *corev1.ConfigMap) { cm.Data[pgconninfo.KeyClusterRWEndpoint] = "stale-blue-rw" }},
		{"publication stamp", func(cm *corev1.ConfigMap) { cm.Annotations[pgconninfo.AnnotationEndpointAttemptID] = "different" }},
		{"superuser selector", func(cm *corev1.ConfigMap) { cm.Data["SUPER_USER_SECRET_REF"] = "blue-superuser" }},
		{"CA selector", func(cm *corev1.ConfigMap) { cm.Data["SERVER_CA_SECRET_REF"] = "blue-ca" }},
		{"missing username", func(cm *corev1.ConfigMap) { delete(cm.Data, "SUPER_USER_NAME") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			coordinator, store, key, proof := committedRecoveryFixture(t)
			receipt := currentBG(store).CommitReceipt.DeepCopy()
			canonical := store.configMaps["cluster-config"]
			canonical.Data["unrelated-key"] = "preserved"
			test.mutate(canonical)
			coordinator.proof = fixedProof{proof: proof, failBlue: true, failGreen: true}
			require.NoError(t, coordinator.RecoverCanonical(t.Context(), key, "attempt"))
			actual := store.configMaps["cluster-config"]
			assert.True(t, endpointDataMatches(actual.Data, proof.Green))
			for key, expected := range proof.GreenAccess {
				assert.Equal(t, expected, actual.Data[key], "prepared access selector %s was not repaired", key)
			}
			assert.Equal(t, "preserved", actual.Data["unrelated-key"])
			assert.Equal(t, receipt, currentBG(store).CommitReceipt)
		})
	}
}

func TestObservedCommitSurvivesAccessSelectorDriftBeforeReceipt(t *testing.T) {
	for _, key := range pgconninfo.ClusterAccessKeys() {
		t.Run(key, func(t *testing.T) {
			coordinator, store, clusterKey, _ := committedRecoveryFixture(t)
			bg := currentBG(store)
			receipt := bg.CommitReceipt.DeepCopy()
			bg.CommitReceipt = nil
			bg.Switchover.EndpointCommittedAt = nil
			store.parent.Status.ProvisionerRef = bg.Blue.Ref.DeepCopy()
			store.parent.Status.BlueGreenCommitSequence = receipt.Sequence - 1
			delete(store.configMaps["cluster-config"].Data, key)

			committed, err := coordinator.ObserveCommit(t.Context(), clusterKey, "attempt")
			require.NoError(t, err)
			require.True(t, committed, "access metadata drift cannot reverse the commit")
			require.ErrorContains(t, coordinator.RestoreUncommittedCanonical(t.Context(), clusterKey, "attempt"), "cannot restore blue after endpoint commit")
			require.False(t, canonicalCommittedFor(store.parent, bg, store.configMaps["cluster-config"]), "the projection still needs repair")

			bg.CommitReceipt = receipt
			require.NoError(t, coordinator.RecoverCanonical(t.Context(), clusterKey, "attempt"))
			require.True(t, canonicalCommittedFor(store.parent, currentBG(store), store.configMaps["cluster-config"]))
		})
	}
}

func TestCoordinatorRejectsMissingPorts(t *testing.T) {
	_, store, _, proof := fixture(t)
	coordinator, err := newTestCoordinator(nil, fixedProof{proof: proof})
	require.Error(t, err)
	assert.Nil(t, coordinator)
	coordinator, err = newTestCoordinator(store, nil)
	require.Error(t, err)
	assert.Nil(t, coordinator)
}

func TestCanonicalOperationsRejectDeletingParentOrProjection(t *testing.T) {
	for _, deleteParent := range []bool{true, false} {
		t.Run(map[bool]string{true: "parent", false: "canonical"}[deleteParent], func(t *testing.T) {
			coordinator, store, key, _ := preparedRecoveryFixture(t)
			now := metav1.Now()
			if deleteParent {
				store.parent.DeletionTimestamp = &now
			} else {
				store.configMaps["cluster-config"].DeletionTimestamp = &now
			}
			require.Error(t, coordinator.CommitCanonical(t.Context(), key, "attempt"))
			_, err := coordinator.ObserveCommit(t.Context(), key, "attempt")
			require.Error(t, err)
			assert.Nil(t, currentBG(store).CommitReceipt)
		})
	}
}

func TestCanonicalRecoveryRemovesObsoleteAccessSelector(t *testing.T) {
	coordinator, store, key, _ := committedRecoveryFixture(t)
	// Optional selectors absent from the committed payload cannot retain a
	// stale value from the previously active environment.
	delete(currentBG(store).EndpointPreparation.AccessMetadata, "SERVER_CA_SECRET_REF")
	store.configMaps["cluster-config"].Data["SERVER_CA_SECRET_REF"] = "obsolete-blue-ca"
	require.NoError(t, coordinator.RecoverCanonical(t.Context(), key, "attempt"))
	assert.NotContains(t, store.configMaps["cluster-config"].Data, "SERVER_CA_SECRET_REF")
}

type delayedCanonicalCommitStore struct {
	*memoryStore
	pending     *corev1.ConfigMap
	commitFirst bool
	commitError error
}

func (s *delayedCanonicalCommitStore) UpdateConfigMap(ctx context.Context, cm *corev1.ConfigMap) error {
	if s.commitFirst {
		s.commitError = s.memoryStore.UpdateConfigMap(ctx, s.pending)
		return s.memoryStore.UpdateConfigMap(ctx, cm)
	}
	if err := s.memoryStore.UpdateConfigMap(ctx, cm); err != nil {
		return err
	}
	s.commitError = s.memoryStore.UpdateConfigMap(ctx, s.pending)
	return nil
}

func TestSourceRestorationInvalidatesUnresolvedCanonicalCommit(t *testing.T) {
	for _, commitFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "restoration wins", true: "commit wins"}[commitFirst], func(t *testing.T) {
			coordinator, store, key, proof := preparedRecoveryFixture(t)
			pending := store.configMaps["cluster-config"].DeepCopy()
			applyPreparedRouting(pending, currentBG(store))
			delayed := &delayedCanonicalCommitStore{memoryStore: store, pending: pending, commitFirst: commitFirst}
			coordinator.store = delayed
			err := coordinator.RestoreUncommittedCanonical(t.Context(), key, "attempt")
			canonical := store.configMaps["cluster-config"]
			if commitFirst {
				require.True(t, apierrors.IsConflict(err), "source restoration cannot proceed after the canonical commit wins")
				require.NoError(t, delayed.commitError)
				assert.True(t, endpointDataMatches(canonical.Data, proof.Green))
				committed, observeErr := coordinator.ObserveCommit(t.Context(), key, "attempt")
				require.NoError(t, observeErr)
				assert.True(t, committed)
			} else {
				require.NoError(t, err)
				assert.True(t, apierrors.IsConflict(delayed.commitError), "an old unresolved commit must conflict before blue can reopen")
				assert.True(t, endpointDataMatches(canonical.Data, proof.Blue))
				assert.NotEmpty(t, canonical.Annotations[pgconninfo.AnnotationSourceRestoreFence])
			}
		})
	}
}

func TestSourceRestorationRequiresSuccessfulCASResponse(t *testing.T) {
	coordinator, store, key, _ := preparedRecoveryFixture(t)
	coordinator.store = &recoveryFailureStore{memoryStore: store, updateLostResponse: true}
	require.ErrorContains(t, coordinator.RestoreUncommittedCanonical(t.Context(), key, "attempt"), "response lost")
	firstVersion := store.configMaps["cluster-config"].ResourceVersion
	require.NoError(t, coordinator.RestoreUncommittedCanonical(t.Context(), key, "attempt"))
	assert.NotEqual(t, firstVersion, store.configMaps["cluster-config"].ResourceVersion)
}

func TestCanonicalRecoveryUsesNormalManagementMetadata(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "target"}[committed], func(t *testing.T) {
			coordinator, store, key, _ := preparedRecoveryFixture(t)
			if committed {
				require.NoError(t, coordinator.CommitCanonical(t.Context(), key, "attempt"))
			}
			delete(store.configMaps, "cluster-config")
			var err error
			if committed {
				err = coordinator.RecoverCanonical(t.Context(), key, "attempt")
			} else {
				err = coordinator.RestoreUncommittedCanonical(t.Context(), key, "attempt")
			}
			require.NoError(t, err)
			cm := store.configMaps["cluster-config"]
			require.NotNil(t, cm)
			assert.Equal(t, pgconninfo.ClusterManagedBy, cm.Labels["app.kubernetes.io/managed-by"])
			assert.True(t, pgendpointprotocol.CanonicalOwnedByParent(store.parent, cm))
		})
	}
}

func TestCoordinatorRequiresAuthorizationAndClock(t *testing.T) {
	_, store, _, proof := fixture(t)
	_, err := NewCoordinator(store, fixedProof{proof: proof}, nil, time.Now)
	require.Error(t, err)
	_, err = NewCoordinator(store, fixedProof{proof: proof}, func(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, time.Time) error {
		return nil
	}, nil)
	require.Error(t, err)
}

func TestCommitUsesCallerAuthorizationAndInjectedClock(t *testing.T) {
	coordinator, store, key, _ := preparedRecoveryFixture(t)
	clock := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	coordinator.now = func() time.Time { return clock }
	rejected := errors.New("caller denied the commit")
	calls := 0
	coordinator.authorize = func(_ context.Context, p *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, now time.Time) error {
		calls++
		require.Equal(t, store.parent.UID, p.UID)
		require.Equal(t, "attempt", bg.AttemptID)
		require.Equal(t, clock, now)
		return rejected
	}
	require.ErrorIs(t, coordinator.CommitCanonical(t.Context(), key, "attempt"), rejected)
	require.Nil(t, currentBG(store).CommitReceipt)
	require.NotEqual(t, "green", store.configMaps["cluster-config"].Annotations[pgconninfo.AnnotationActiveEnvironment])
	require.Equal(t, 1, calls)
	// A different workflow can authorize the same generic endpoint protocol. It
	// does not need to manufacture switchover receipts understood by this package.
	currentBG(store).Switchover = nil
	currentBG(store).Gates = nil
	coordinator.authorize = func(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, time.Time) error {
		return nil
	}
	require.NoError(t, coordinator.CommitCanonical(t.Context(), key, "attempt"))
	require.Equal(t, clock, currentBG(store).CommitReceipt.CommittedAt.Time)
	// Recovery repairs an existing commit without re-running first-commit policy.
	coordinator.authorize = func(context.Context, *platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, time.Time) error {
		return rejected
	}
	require.NoError(t, coordinator.CommitCanonical(t.Context(), key, "attempt"))
}

func TestCommitReauthorizesDeadlineAfterPreparationBeforeCanonicalCAS(t *testing.T) {
	coordinator, store, key, _ := preparedRecoveryFixture(t)
	window := currentBG(store).Switchover
	clockCalls := 0
	coordinator.now = func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return window.StartedAt.Time
		}
		return window.DeadlineAt.Time
	}
	canonical := store.configMaps["cluster-config"].DeepCopy()
	calls := 0
	coordinator.authorize = func(_ context.Context, _ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, now time.Time) error {
		calls++
		return mvutypes.AuthorizeCutoverCommit(bg, now)
	}

	require.ErrorIs(t, coordinator.CommitCanonical(t.Context(), key, "attempt"), context.DeadlineExceeded)
	require.Equal(t, 2, calls)
	require.Equal(t, canonical, store.configMaps["cluster-config"], "the final authorization must precede the irreversible write")
	require.Nil(t, currentBG(store).CommitReceipt)
}
