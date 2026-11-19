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
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	dbcnpg "github.com/splunk/splunk-operator/pkg/postgresql/database/adapter/cnpg"
	dbcore "github.com/splunk/splunk-operator/pkg/postgresql/database/core"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func (f *cleanupFixture) step(t *testing.T) bluegreen.Outcome {
	t.Helper()
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.parent), f.parent))
	store := NewMajorUpgradeStateStore(newTestClusterStateStore(t, f.client, client.ObjectKeyFromObject(f.parent)))
	intent, enabled, err := store.ReadMajorUpgradeIntent(t.Context())
	require.NoError(t, err)
	require.True(t, enabled)
	out, err := f.runtime.AdvanceCleanup(t.Context(), intent)
	require.NoError(t, err)
	_, err = store.SaveMajorUpgradeProgress(t.Context(), intent, mvutypes.Progress{Report: out.Report, BlueGreen: out.BlueGreen})
	require.NoError(t, err)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.parent), f.parent))
	return out
}

func TestCleanupRuntimeWaitsForChildProofAndProviderAbsence(t *testing.T) {
	f := newCleanupFixture(t)
	child := &platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "ns", UID: "child-uid", Generation: 2}, Spec: platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: f.parent.Name}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "app"}}}}
	database := &cnpgv1.Database{ObjectMeta: metav1.ObjectMeta{Name: "declaration", Namespace: "ns", UID: "db-uid", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(child, platformv1alpha1.GroupVersion.WithKind("PostgresDatabase"))}}, Spec: cnpgv1.DatabaseSpec{Name: "app", ClusterRef: corev1.LocalObjectReference{Name: f.retired.Name}}}
	require.NoError(t, f.client.Create(t.Context(), child))
	require.NoError(t, f.client.Create(t.Context(), database))
	first := f.step(t)
	require.NotNil(t, first.BlueGreen.Gates.Cleanup)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), &cnpgv1.Cluster{}))
	cleanup := first.BlueGreen.Cleanup
	child.Status.EnvironmentRetirement = &platformv1alpha1.PostgresDatabaseEnvironmentRetirementStatus{ParentUID: f.parent.UID, AttemptID: first.BlueGreen.AttemptID, RetirementGeneration: *cleanup.RetirementGeneration, ObservedDatabaseGeneration: child.Generation, EnvironmentRef: *cleanup.EnvironmentRef, Databases: []platformv1alpha1.DatabaseEnvironmentRetirementStatus{{DatabaseName: "app", Resources: cleanup.RetirementAcknowledgements[0].Resources, AcknowledgedAt: new(metav1.Now())}}}
	require.NoError(t, f.client.Status().Update(t.Context(), child))
	f.step(t)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), &cnpgv1.Cluster{}), "child proof alone cannot authorize deletion")
	require.NoError(t, f.client.Delete(t.Context(), database))
	f.step(t)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), &cnpgv1.Cluster{}), "receipts must persist first")
	f.parent.Spec.PostgresMajorUpgradeConfig.BlueGreen.Cleanup = new(false)
	require.NoError(t, f.client.Update(t.Context(), f.parent))
	f.step(t)
	require.True(t, apierrors.IsNotFound(f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), &cnpgv1.Cluster{})))
	out := f.step(t)
	assert.Equal(t, platformv1alpha1.BlueGreenCleanupStateCleaned, out.BlueGreen.Cleanup.State)
	assert.Equal(t, "Cancelled", out.Report.Phase)
	assert.False(t, out.Report.Retry)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.active), &cnpgv1.Cluster{}))
}

func TestCleanupComposesChildRetirementAcrossAcknowledgementFailureAndDrift(t *testing.T) {
	f := newCleanupFixture(t)
	child := &platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "ns", UID: "child-uid", Generation: 1}, Spec: platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: f.parent.Name}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "app"}}}}
	declaration := &cnpgv1.Database{ObjectMeta: metav1.ObjectMeta{Name: "retired-database", Namespace: "ns", UID: "declaration-uid", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(child, platformv1alpha1.GroupVersion.WithKind("PostgresDatabase"))}}, Spec: cnpgv1.DatabaseSpec{Name: "app", ClusterRef: corev1.LocalObjectReference{Name: f.retired.Name}, ReclaimPolicy: cnpgv1.DatabaseReclaimDelete}}
	require.NoError(t, f.client.Create(t.Context(), child))
	require.NoError(t, f.client.Create(t.Context(), declaration))
	f.step(t)
	failedWrite := errors.New("acknowledgement write rejected")
	rejectAcknowledgement := true
	writes := 0
	guarded := interceptor.NewClient(f.client.(client.WithWatch), interceptor.Funcs{SubResourcePatch: func(ctx context.Context, next client.Client, subresource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
		if database, ok := obj.(*platformv1alpha1.PostgresDatabase); ok && subresource == "status" && database.Status.EnvironmentRetirement != nil {
			writes++
			if rejectAcknowledgement {
				return failedWrite
			}
		}
		return next.SubResource(subresource).Patch(ctx, obj, patch, opts...)
	}})
	rc := &dbcore.ReconcileContext{Client: guarded, APIReader: f.client, Scheme: f.client.Scheme(), EnvironmentRetirer: dbcnpg.NewEnvironmentRetirer(guarded, f.client)}
	childPass := func() error {
		t.Helper()
		require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(child), child))
		_, err := dbcore.PostgresDatabaseService(t.Context(), rc, child, nil)
		return err
	}
	assertRetiredPresent := func() {
		t.Helper()
		require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), f.retired))
		assert.Nil(t, f.retired.DeletionTimestamp)
	}
	require.NoError(t, childPass())
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(declaration), declaration))
	assert.Equal(t, cnpgv1.DatabaseReclaimRetain, declaration.Spec.ReclaimPolicy)
	f.step(t)
	assertRetiredPresent()
	require.NoError(t, childPass())
	require.True(t, apierrors.IsNotFound(f.client.Get(t.Context(), client.ObjectKeyFromObject(declaration), &cnpgv1.Database{})))
	require.ErrorIs(t, childPass(), failedWrite)
	require.Equal(t, 1, writes)
	f.step(t)
	assertRetiredPresent()
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(child), child))
	assert.Nil(t, child.Status.EnvironmentRetirement)
	rejectAcknowledgement = false
	require.NoError(t, childPass())
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(child), child))
	require.NotNil(t, child.Status.EnvironmentRetirement)
	oldGeneration := child.Status.EnvironmentRetirement.ObservedDatabaseGeneration
	child.Generation++
	require.NoError(t, f.client.Update(t.Context(), child))
	f.step(t)
	f.step(t)
	assertRetiredPresent()
	assert.Equal(t, oldGeneration+1, f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Cleanup.RetirementAcknowledgements[0].PostgresDatabaseGeneration)
	require.NoError(t, childPass())
	f.step(t) // Collect and persist the fresh child acknowledgement before deletion.
	assertRetiredPresent()
	f.step(t)
	require.True(t, apierrors.IsNotFound(f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), &cnpgv1.Cluster{})))
	completed := f.step(t)
	assert.Equal(t, platformv1alpha1.BlueGreenCleanupStateCleaned, completed.BlueGreen.Cleanup.State)
	assert.Equal(t, "Cancelled", completed.Report.Phase)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.active), &cnpgv1.Cluster{}))
}

func TestCleanupRuntimeReissuesBarrierForChangedChild(t *testing.T) {
	f := newCleanupFixture(t)
	child := &platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "ns", UID: "child-uid", Generation: 2}, Spec: platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: f.parent.Name}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "app"}}}}
	require.NoError(t, f.client.Create(t.Context(), child))
	first := f.step(t)
	child.Generation++
	require.NoError(t, f.client.Update(t.Context(), child))
	second := f.step(t)
	assert.Equal(t, *first.BlueGreen.Cleanup.RetirementGeneration+1, *second.BlueGreen.Cleanup.RetirementGeneration)
	assert.Equal(t, child.Generation, second.BlueGreen.Cleanup.RetirementAcknowledgements[0].PostgresDatabaseGeneration)
	assert.Nil(t, second.BlueGreen.Cleanup.RetirementAcknowledgements[0].AcknowledgedAt)
	require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), &cnpgv1.Cluster{}))
}

func TestCleanupFreshGuardRejectsDriftBeforeMutation(t *testing.T) {
	for _, change := range []string{"authority", "routing", "child generation"} {
		t.Run(change, func(t *testing.T) {
			f := newCleanupFixture(t)
			child := &platformv1alpha1.PostgresDatabase{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "ns", UID: "child-uid", Generation: 1}, Spec: platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: f.parent.Name}, Databases: []platformv1alpha1.DatabaseDefinition{{Name: "app"}}}}
			require.NoError(t, f.client.Create(t.Context(), child))
			bg := f.step(t).BlueGreen
			child.Status.EnvironmentRetirement = &platformv1alpha1.PostgresDatabaseEnvironmentRetirementStatus{ParentUID: f.parent.UID, AttemptID: bg.AttemptID, RetirementGeneration: *bg.Cleanup.RetirementGeneration, ObservedDatabaseGeneration: child.Generation, EnvironmentRef: *bg.Cleanup.EnvironmentRef, Databases: []platformv1alpha1.DatabaseEnvironmentRetirementStatus{{DatabaseName: "app", AcknowledgedAt: new(metav1.Now())}}}
			require.NoError(t, f.client.Status().Update(t.Context(), child))
			f.step(t) // Persist the parent's collection of the child proof.
			store := NewMajorUpgradeStateStore(newTestClusterStateStore(t, f.client, client.ObjectKeyFromObject(f.parent)))
			intent, enabled, err := store.ReadMajorUpgradeIntent(t.Context())
			require.NoError(t, err)
			require.True(t, enabled)
			reads, deletes := 0, 0
			guarded := interceptor.NewClient(f.client.(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, next client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*platformv1alpha1.PostgresCluster); ok {
						reads++
						if reads == 2 {
							switch change {
							case "authority":
								f.parent.Status.ProvisionerRef = bg.Green.Ref.DeepCopy()
								require.NoError(t, next.Status().Update(ctx, f.parent))
							case "routing":
								canonical := &corev1.ConfigMap{}
								require.NoError(t, next.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "canonical"}, canonical))
								canonical.Data[pgconninfo.KeyClusterRWEndpoint] = "retired-rw.ns.svc.cluster.local"
								require.NoError(t, next.Update(ctx, canonical))
							case "child generation":
								child.Generation++
								require.NoError(t, next.Update(ctx, child))
							}
						}
					}
					return next.Get(ctx, key, obj, opts...)
				},
				Delete: func(ctx context.Context, next client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					deletes++
					return next.Delete(ctx, obj, opts...)
				},
			})
			f.runtime.reader, f.runtime.client = guarded, guarded
			_, err = f.runtime.AdvanceCleanup(t.Context(), intent)
			require.Error(t, err)
			require.GreaterOrEqual(t, reads, 2, "drift must occur after planning")
			assert.Zero(t, deletes)
			require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), f.retired))
			assert.Nil(t, f.retired.DeletionTimestamp)
		})
	}
}

func TestCleanupSnapshotListFailuresProtectRetiredCluster(t *testing.T) {
	for _, test := range []struct {
		name          string
		err           error
		permitsDelete bool
	}{
		{"CRD absent", &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: snapshotv1.GroupName, Kind: "VolumeSnapshot"}}, true},
		{"forbidden", apierrors.NewForbidden(schema.GroupResource{Group: snapshotv1.GroupName, Resource: "volumesnapshots"}, "", errors.New("denied")), false},
		{"list failed", errors.New("snapshot API unavailable"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newCleanupFixture(t)
			f.step(t)
			lists := 0
			guarded := interceptor.NewClient(f.client.(client.WithWatch), interceptor.Funcs{List: func(ctx context.Context, next client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*snapshotv1.VolumeSnapshotList); ok {
					lists++
					return test.err
				}
				return next.List(ctx, list, opts...)
			}})
			f.runtime.reader = guarded
			store := NewMajorUpgradeStateStore(newTestClusterStateStore(t, f.client, client.ObjectKeyFromObject(f.parent)))
			intent, _, err := store.ReadMajorUpgradeIntent(t.Context())
			require.NoError(t, err)
			_, err = f.runtime.AdvanceCleanup(t.Context(), intent)
			require.Equal(t, 1, lists)
			liveErr := f.client.Get(t.Context(), client.ObjectKeyFromObject(f.retired), f.retired)
			if test.permitsDelete {
				require.NoError(t, err)
				assert.True(t, apierrors.IsNotFound(liveErr))
			} else {
				require.ErrorIs(t, err, test.err)
				require.NoError(t, liveErr)
				assert.Nil(t, f.retired.DeletionTimestamp)
			}
		})
	}
}

func TestCleanupRuntimeCompletesWithoutChildren(t *testing.T) {
	for _, phase := range []string{"Completed", "Cancelled"} {
		t.Run(phase, func(t *testing.T) {
			f := newCleanupFixture(t)
			if phase == "Completed" {
				bg := f.parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen
				bg.Blue, bg.Green = bg.Green, bg.Blue
				now := metav1.Now()
				bg.Gates = nil
				bg.CommitReceipt = &platformv1alpha1.BlueGreenCommitReceiptStatus{Target: bg.Green.Ref.DeepCopy(), CommittedAt: &now, Sequence: 1, CanonicalConfigMap: &corev1.ObjectReference{Name: "canonical", UID: "canonical-uid"}}
				bg.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{TargetAvailableAt: &now, EndpointPublicationEventRef: &corev1.ObjectReference{Name: "published"}}
				bg.Completion = &platformv1alpha1.BlueGreenCompletionStatus{StartedAt: &now, SourceStoppedAt: &now, ReplicationRemovedAt: &now, SourceCredentialsRetiredAt: &now, CredentialsRemovedAt: &now}
				bg.Recoverability = &platformv1alpha1.BlueGreenRecoverabilityStatus{TargetBackupName: new("target-anchor"), BackupVerifiedAt: &now, WALArchiveBaselineVerifiedAt: &now}
				f.parent.Status.PostgresMajorUpgradeStatus[0].Phase = new(phase)
				require.NoError(t, f.client.Status().Update(t.Context(), f.parent))
			}
			for range 4 {
				out := f.step(t)
				if out.BlueGreen.Cleanup.State == platformv1alpha1.BlueGreenCleanupStateCleaned {
					assert.Equal(t, phase, out.Report.Phase)
					require.NoError(t, f.client.Get(t.Context(), client.ObjectKeyFromObject(f.active), &cnpgv1.Cluster{}))
					return
				}
			}
			t.Fatal("cleanup did not converge")
		})
	}
}
