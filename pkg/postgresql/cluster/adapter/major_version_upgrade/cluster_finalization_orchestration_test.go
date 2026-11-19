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
	"errors"
	"strconv"
	"testing"

	certv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

type finalizationDrainSQL struct {
	cutoverSQL
	ready bool
	err   error
	calls int
}

func (s *finalizationDrainSQL) DrainRetainedSource(context.Context, *cnpgv1.Cluster, cutoverIdentity, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	s.calls++
	return s.ready, s.err
}

type finalizationSQLObservations struct {
	access            *accessSQLStub
	drain             *finalizationDrainSQL
	completion        *completionSQLFake
	source            *teardownSQLFake
	subscriberCalls   int
	subscriberPending bool
}

func finalizationOrchestrationFixture(t *testing.T, committed bool) (*cleanupFixture, *ClusterFinalizer, *finalizationSQLObservations) {
	t.Helper()
	a, blue, green, databases, bg, accessSQL := accessFixture(t)
	require.NoError(t, a.Snapshot(t.Context(), blue, green, databases, bg))
	require.NoError(t, a.PrepareControl(t.Context(), blue, green, databases, bg))
	require.NoError(t, a.ApplyFence(t.Context(), blue, green, bg))
	for _, cluster := range []*cnpgv1.Cluster{blue, green} {
		require.NoError(t, a.client.Get(t.Context(), client.ObjectKeyFromObject(cluster), cluster))
		cluster.Status.WriteService, cluster.Status.ReadService = cluster.Name+"-rw", cluster.Name+"-ro"
		cluster.Status.ReadyInstances, cluster.Status.SystemID = 2, "111"
		cluster.Status.Phase, cluster.Status.CurrentPrimary = cnpgv1.PhaseHealthy, cluster.Name+"-1"
		cluster.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {cluster.Status.CurrentPrimary}}
		require.NoError(t, a.client.Update(t.Context(), cluster))
	}
	parent := a.owner.DeepCopy()
	parent.Spec.ClusterDeletionPolicy = new("Retain")
	parent.Finalizers = []string{"test/finalizer"}
	parent.Status.Resources = &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "canonical"}}
	parent.Status.ProvisionerRef = bg.Blue.Ref.DeepCopy()
	now := metav1.Now()
	bg.Switchover.StartedAt, bg.Switchover.FenceVerifiedAt, bg.Switchover.DrainCompletedAt = &now, &now, &now
	bg.Switchover.FinalBlueLSN = new("0/100")
	bg.DatabaseReplication = []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{DatabaseName: "orders"}}
	bg.EndpointIntentRevision = 1
	routing := func(cluster *cnpgv1.Cluster) map[string]string {
		data, err := pgconninfo.RoutingData(pgconninfo.Endpoints{RWHost: cluster.Name + "-rw.default.svc.cluster.local", ROHost: cluster.Name + "-ro.default.svc.cluster.local", RHost: cluster.Name + "-r.default.svc.cluster.local"})
		require.NoError(t, err)
		return data
	}
	greenRouting := routing(green)
	bg.EndpointPreparation = &platformv1alpha1.BlueGreenEndpointPreparationStatus{Generation: 1, EndpointFingerprint: new(pgconninfo.FingerprintEndpointData(greenRouting)), Routing: greenRouting}
	canonical := pgconninfo.NewClusterConfigMap(parent, "canonical", routing(blue))
	canonical.UID = "canonical-uid"
	if committed {
		parent.Status.ProvisionerRef = bg.Green.Ref.DeepCopy()
		canonical.Data = greenRouting
		canonical.Annotations = map[string]string{
			pgconninfo.AnnotationActiveEnvironment: green.Name, pgconninfo.AnnotationEndpointAttemptID: bg.AttemptID,
			pgconninfo.AnnotationEndpointIntentGeneration: strconv.FormatInt(bg.EndpointPreparation.Generation, 10), pgconninfo.AnnotationEndpointFingerprint: *bg.EndpointPreparation.EndpointFingerprint,
		}
		bg.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1, CommittedAt: &now, CanonicalConfigMap: &corev1.ObjectReference{Name: canonical.Name, UID: canonical.UID}, Target: bg.Green.Ref.DeepCopy()}
		parent.Status.BlueGreenCommitSequence = 1
	}
	parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: new("SwitchingOver"), Strategy: new("blueGreen"), SourcePgVersion: new("17"), TargetPgVersion: new("18"), BlueGreen: bg}}
	objects, err := namespaceObjects(t.Context(), a.client, parent.Namespace, &corev1.SecretList{}, &cnpgv1.ClusterList{})
	require.NoError(t, err)
	objects = append(objects, parent, canonical)
	require.NoError(t, certv1.AddToScheme(a.client.Scheme()))
	require.NoError(t, appsv1.AddToScheme(a.client.Scheme()))
	c := fake.NewClientBuilder().WithScheme(a.client.Scheme()).WithStatusSubresource(parent).WithObjects(objects...).Build()
	require.NoError(t, c.Delete(t.Context(), parent))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	f := &cleanupFixture{client: c, parent: parent, active: green, retired: blue}
	observations := &finalizationSQLObservations{access: accessSQL, drain: &finalizationDrainSQL{ready: true}, completion: &completionSQLFake{}, source: &teardownSQLFake{slotsGone: true}}
	runtime := NewClusterFinalizer(c, c, nil)
	runtime.runtime = func(parent *platformv1alpha1.PostgresCluster) (*finalizationSQLRuntime, error) {
		ports, err := runtime.newSQLRuntime(parent)
		if err != nil {
			return nil, err
		}
		s := ports.cancellation.switchover
		s.sql = observations.drain
		s.access.(*blueGreenAccess).sql = observations.access
		ports.cancellation.sql = observations.source
		ports.cancellation.subscriberSQL = subscriptionSQLFunc(func(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
			observations.subscriberCalls++
			return !observations.subscriberPending, nil
		})
		ports.completion.sql = observations.completion
		return ports, nil
	}
	return f, runtime, observations
}

func TestFinalizationRevalidatesFenceAfterSQLDone(t *testing.T) {
	for _, detached := range []bool{false, true} {
		t.Run(strconv.FormatBool(detached), func(t *testing.T) {
			f, runtime, _ := finalizationOrchestrationFixture(t, true)
			for range 15 {
				_, err := finalizationStep(t, f, runtime)
				require.NoError(t, err)
				if f.parent.Status.Finalization.SQLDone {
					break
				}
			}
			require.True(t, f.parent.Status.Finalization.SQLDone)
			if detached {
				_, err := finalizationStep(t, f, runtime)
				require.NoError(t, err)
			}
			blue := &cnpgv1.Cluster{}
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), blue))
			blue.Spec.PostgresConfiguration.PgHBA = []string{"host all all all trust"}
			require.NoError(t, f.client.Update(t.Context(), blue))
			writes := 0
			guarded := interceptor.NewClient(f.client.(client.WithWatch), interceptor.Funcs{Patch: func(ctx context.Context, next client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				writes++
				return next.Patch(ctx, obj, patch, opts...)
			}})
			restarted := NewClusterFinalizer(guarded, f.client, nil)
			done, err := finalizationStep(t, f, restarted)
			require.ErrorContains(t, err, "retained blue application fence changed")
			assert.False(t, done)
			assert.Zero(t, writes, "SQLDone cannot authorize another ownership mutation after fence drift")
		})
	}
}

func TestFinalizationReprovesSQLAfterDeclarationDeletionAndRestart(t *testing.T) {
	f, runtime, observed := finalizationOrchestrationFixture(t, false)
	bg := f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen
	now := metav1.Now()
	bg.Gates = &platformv1alpha1.BlueGreenGateStatus{Cancellation: &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: &now, GreenReplicationRemovedAt: &now, GreenReplicationProofVersion: greenReplicationProofVersion}}
	require.NoError(t, f.client.Status().Update(t.Context(), f.parent))
	declaration := &cnpgv1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "subscription", Namespace: f.parent.Namespace, UID: "original-writer", Labels: map[string]string{blueGreenAttemptLabel: bg.AttemptID}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(f.parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}}}
	require.NoError(t, f.client.Create(t.Context(), declaration))
	observed.access.openReady = true
	observed.subscriberPending = true
	for range 15 {
		_, err := finalizationStep(t, f, runtime)
		require.NoError(t, err)
		if observed.subscriberCalls > 0 {
			break
		}
	}
	require.True(t, apierrors.IsNotFound(f.client.Get(t.Context(), client.ObjectKeyFromObject(declaration), &cnpgv1.Subscription{})))
	require.Positive(t, observed.subscriberCalls)
	assert.False(t, f.parent.Status.Finalization.SQLDone)
	assert.Empty(t, observed.source.slotCalls, "an old receipt cannot bypass the fresh subscriber proof")
	observed.subscriberPending = false
	for range 15 {
		_, err := finalizationStep(t, f, runtime)
		require.NoError(t, err)
		if f.parent.Status.Finalization.SQLDone {
			break
		}
	}
	require.True(t, f.parent.Status.Finalization.SQLDone)
	require.NotEmpty(t, observed.source.slotCalls)
	calls := observed.subscriberCalls
	// A fresh finalizer must accept the saved proof although the inventory keeps old references.
	restarted := NewClusterFinalizer(f.client, f.client, nil)
	complete := false
	for range 15 {
		var err error
		complete, err = finalizationStep(t, f, restarted)
		require.NoError(t, err)
		if complete {
			break
		}
	}
	require.True(t, complete)
	assert.Equal(t, calls, observed.subscriberCalls)
	// The same declaration name with a new UID is a new writer, not the old historical reference.
	declaration.UID, declaration.ResourceVersion = "reappeared-writer", ""
	declaration.Spec.ReclaimPolicy = cnpgv1.SubscriptionReclaimRetain
	require.NoError(t, f.client.Create(t.Context(), declaration))
	observed.subscriberPending = true
	for range 8 {
		_, err := finalizationStep(t, f, runtime)
		require.NoError(t, err)
		if observed.subscriberCalls > calls {
			break
		}
	}
	require.Greater(t, observed.subscriberCalls, calls)
	assert.False(t, f.parent.Status.Finalization.SQLDone)
}

func TestFinalizationCommittedAccessPrecedesSQLProofAndOrphaning(t *testing.T) {
	f, runtime, observed := finalizationOrchestrationFixture(t, true)
	assertOwned := func() {
		for _, environment := range []*cnpgv1.Cluster{f.active, f.retired} {
			live := &cnpgv1.Cluster{}
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(environment), live))
			assert.True(t, metav1.IsControlledBy(live, f.parent))
		}
	}
	observed.drain.ready = false
	for range 3 {
		done, err := finalizationStep(t, f, runtime)
		require.NoError(t, err)
		assert.False(t, done)
	}
	assert.Positive(t, observed.drain.calls)
	assert.Zero(t, observed.subscriberCalls)
	assert.False(t, f.parent.Status.Finalization.SQLDone)
	assertOwned()
	observed.drain.err = errors.New("drain SQL unavailable")
	_, err := finalizationStep(t, f, runtime)
	require.ErrorContains(t, err, "drain SQL unavailable")
	assertOwned()
	observed.drain.ready, observed.drain.err, observed.access.openReady = true, nil, false
	for range 5 {
		_, err = finalizationStep(t, f, runtime)
		require.NoError(t, err)
	}
	bg := f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen
	require.NotNil(t, bg.Switchover.BlueSessionsTerminatedAt)
	assert.Nil(t, bg.Switchover.GreenFenceRemovedAt)
	assert.False(t, f.parent.Status.Finalization.SQLDone)
	assert.Zero(t, observed.subscriberCalls)
	assertOwned()
	observed.access.openReady = true
	for range 10 {
		_, err = finalizationStep(t, f, runtime)
		require.NoError(t, err)
		if f.parent.Status.Finalization.SQLDone {
			break
		}
	}
	require.True(t, f.parent.Status.Finalization.SQLDone)
	bg = f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen
	require.NotNil(t, bg.Switchover.GreenFenceRemovedAt)
	assert.Nil(t, bg.Switchover.TargetAvailableAt, "finalization access proof must not claim endpoint publication")
	assert.Positive(t, observed.subscriberCalls)
	assert.Positive(t, observed.completion.sourceCalls)
	probes, drains := observed.subscriberCalls, observed.drain.calls
	complete := false
	for range 20 {
		next := NewClusterFinalizer(f.client, f.client, nil)
		next.runtime = runtime.runtime
		complete, err = finalizationStep(t, f, next)
		require.NoError(t, err)
		if complete {
			break
		}
	}
	require.True(t, complete)
	for _, cluster := range []*cnpgv1.Cluster{f.active, f.retired} {
		require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(cluster), cluster))
		assert.Empty(t, cluster.OwnerReferences)
	}
	assert.True(t, accessMatches(f.active, bg, sideGreen, fenceOpen))
	assert.True(t, retiredAccessMatches(f.retired, bg))
	assert.Equal(t, probes, observed.subscriberCalls, "partial orphan retries reuse the persisted SQL proof")
	assert.Equal(t, drains, observed.drain.calls)
}

func TestFinalizationRecoversCommitWhoseReceiptWriteWasLost(t *testing.T) {
	for _, missingBlue := range []bool{false, true} {
		t.Run(strconv.FormatBool(missingBlue), func(t *testing.T) {
			f, runtime, observed := finalizationOrchestrationFixture(t, true)
			if missingBlue {
				require.NoError(t, f.client.Delete(t.Context(), f.retired))
			}
			f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.CommitReceipt = nil
			f.parent.Status.BlueGreenCommitSequence = 0
			f.parent.Status.ProvisionerRef = f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Blue.Ref.DeepCopy()
			require.NoError(t, f.client.Status().Update(t.Context(), f.parent))
			complete := false
			for range 24 {
				var err error
				complete, err = finalizationStep(t, f, runtime)
				require.NoError(t, err)
				if complete {
					break
				}
			}
			require.True(t, complete)
			bg := f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen
			require.NotNil(t, bg.CommitReceipt)
			assert.Equal(t, bg.Green.Ref.UID, bg.CommitReceipt.Target.UID)
			assert.NotNil(t, bg.Switchover.BlueSessionsTerminatedAt)
			assert.NotNil(t, bg.Switchover.GreenFenceRemovedAt)
			if missingBlue {
				assert.Zero(t, observed.drain.calls)
			} else {
				assert.Positive(t, observed.drain.calls)
			}
		})
	}
}

func TestFinalizationRequiresHistoricalSQLProof(t *testing.T) {
	for _, proved := range []bool{false, true} {
		t.Run(strconv.FormatBool(proved), func(t *testing.T) {
			f, runtime, observed := finalizationOrchestrationFixture(t, true)
			entry := &f.parent.Status.PostgresMajorUpgradeStatus[0]
			entry.Phase = new("Completed")
			bg, now := entry.BlueGreen, metav1.Now()
			bg.Switchover.TargetAvailableAt, bg.Switchover.GreenFenceRemovedAt, bg.Switchover.BlueSessionsTerminatedAt = &now, &now, &now
			bg.Switchover.EndpointPublicationEventRef = &corev1.ObjectReference{Name: "published", UID: "event-uid"}
			bg.Recoverability = &platformv1alpha1.BlueGreenRecoverabilityStatus{TargetBackupName: new("anchor"), BackupVerifiedAt: &now, WALArchiveBaselineVerifiedAt: &now}
			bg.Completion = &platformv1alpha1.BlueGreenCompletionStatus{StartedAt: &now, SourceStoppedAt: &now, SourceCredentialsRetiredAt: &now, CredentialsRemovedAt: &now}
			if proved {
				bg.Completion.ReplicationRemovedAt = &now
			}
			f.parent.Status.PostgresMajorUpgradeStatus = append(f.parent.Status.PostgresMajorUpgradeStatus, platformv1alpha1.PostgresMajorUpgradeStatus{Strategy: new("pgUpgrade"), Phase: new("Completed")})
			require.NoError(t, f.client.Status().Update(t.Context(), f.parent))
			_, err := finalizationStep(t, f, runtime)
			require.NoError(t, err)
			_, err = finalizationStep(t, f, runtime)
			if proved {
				require.NoError(t, err)
				require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.parent), f.parent))
				assert.True(t, f.parent.Status.Finalization.SQLDone)
			} else {
				require.ErrorContains(t, err, "historical SQL teardown proof")
				assert.False(t, f.parent.Status.Finalization.SQLDone)
			}
			assert.Zero(t, observed.drain.calls)
			assert.Zero(t, observed.subscriberCalls)
		})
	}
}

func TestFinalizationPrecommitRestorationUsesRealStatusCheckpoints(t *testing.T) {
	f, runtime, observed := finalizationOrchestrationFixture(t, false)
	observed.access.openReady = false
	for range 5 {
		_, err := finalizationStep(t, f, runtime)
		require.NoError(t, err)
	}
	bg := f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen
	require.NotNil(t, bg.Gates.Cancellation)
	assert.Nil(t, bg.Gates.Cancellation.SourceAccessRestoredAt)
	assert.False(t, f.parent.Status.Finalization.SQLDone)
	assert.Zero(t, observed.subscriberCalls)
	observed.access.openReady = true
	complete := false
	for range 24 {
		var err error
		complete, err = finalizationStep(t, f, runtime)
		require.NoError(t, err)
		if complete {
			break
		}
	}
	require.True(t, complete, "SQLDone=%v cancellation=%+v source calls=%v subscriber=%d", f.parent.Status.Finalization.SQLDone, f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Gates.Cancellation, observed.source.stopCalls, observed.subscriberCalls)
	bg = f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen
	assert.Nil(t, bg.CommitReceipt)
	require.NotNil(t, bg.Gates.Cancellation.SourceAccessRestoredAt)
	require.NotNil(t, bg.Switchover.SourceFenceRestoredAt)
	assert.Equal(t, int32(1), bg.Gates.Cancellation.GreenReplicationProofVersion)
	assert.Positive(t, observed.subscriberCalls)
	assert.Zero(t, observed.drain.calls, "precommit restoration must not drain committed blue")
	assert.Empty(t, observed.source.roleCalls, "Retain preserves maintenance credentials")
}
