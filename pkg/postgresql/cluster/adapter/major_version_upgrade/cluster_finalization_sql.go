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
	"slices"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type finalizationSQLRuntime struct {
	endpoints    recoveryEndpoints
	cancellation *BlueGreenCancellation
	completion   *BlueGreenCompletion
}

func (f *ClusterFinalizer) newSQLRuntime(parent *platformv1alpha1.PostgresCluster) (*finalizationSQLRuntime, error) {
	s, err := NewBlueGreenSwitchover(f.client, f.reader, client.ObjectKeyFromObject(parent), parent, BlueGreenCandidatePlan{})
	if err != nil {
		return nil, err
	}
	endpoints, err := NewEndpointCoordinator(f.client, f.reader)
	if err != nil {
		return nil, err
	}
	recovery := endpoints.ForFinalization(func(parent *platformv1alpha1.PostgresCluster, cm *corev1.ConfigMap) bool {
		return finalizationOwns(parent, cm)
	})
	c, err := NewBlueGreenCancellation(f.client, f.reader, client.ObjectKeyFromObject(parent), parent, s)
	if err != nil {
		return nil, err
	}
	completion, err := NewBlueGreenCompletion(s, c.BlueGreenReplicationTeardown, nil)
	if err != nil {
		return nil, err
	}
	return &finalizationSQLRuntime{cancellation: c, completion: completion, endpoints: recovery}, nil
}

func (f *ClusterFinalizer) teardownSQL(ctx context.Context, parent *platformv1alpha1.PostgresCluster) (bool, error) {
	refreshSQL := finalizationRequiresFreshSQL(parent)
	if refreshSQL && !slices.ContainsFunc(parent.Status.PostgresMajorUpgradeStatus, func(entry platformv1alpha1.PostgresMajorUpgradeStatus) bool {
		return entry.BlueGreen != nil && !mvutypes.IsBlueGreenAttemptCleaned(entry)
	}) {
		return false, fmt.Errorf("cleaned attempts cannot prove SQL absence for later replication declarations")
	}
	for i, entry := range parent.Status.PostgresMajorUpgradeStatus {
		bg := entry.BlueGreen
		if bg == nil || mvutypes.IsBlueGreenAttemptCleaned(entry) {
			continue
		}
		if i != len(parent.Status.PostgresMajorUpgradeStatus)-1 && mvutypes.BlueGreenCleanupEligible(entry) && completionReplicationRemoved(parent, bg) {
			continue
		}
		if mvutypes.BlueGreenCleanupEligible(entry) && bg.Gates != nil && bg.Gates.Cancellation != nil && bg.Gates.Cancellation.GreenReplicationRemovedAt != nil && bg.Gates.Cancellation.GreenReplicationProofVersion == greenReplicationProofVersion && !finalizationRequiresFreshSQL(parent) {
			continue
		}
		if i != len(parent.Status.PostgresMajorUpgradeStatus)-1 {
			return false, fmt.Errorf("attempt %s lacks historical SQL teardown proof", bg.AttemptID)
		}
		bg = bg.DeepCopy()
		// Discover a lost candidate write by ownership/attempt, independent of its name.
		if bg.Green == nil {
			candidates, err := discoverAttemptCandidates(ctx, f.reader, parent, bg)
			if err != nil {
				return false, err
			}
			if len(candidates) > 1 {
				return false, fmt.Errorf("attempt %s has ambiguous candidate identities", bg.AttemptID)
			}
			if len(candidates) == 1 {
				ref, err := resourceReference(&candidates[0])
				if err != nil {
					return false, err
				}
				bg.Green = &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: ref}
			}
			if bg.Green != nil {
				return false, f.saveFinalizationEvidence(ctx, parent, bg)
			}
		}
		runtime, err := f.runtime(parent)
		if err != nil {
			return false, err
		}
		c, completion := runtime.cancellation, runtime.completion
		s := c.switchover
		committed := bg.CommitReceipt != nil
		if bg.Blue != nil && bg.Green != nil {
			committed, err = runtime.endpoints.ObserveCommit(ctx, s.key, bg.AttemptID)
			if err != nil {
				return false, err
			}
		}
		if committed {
			if bg.CommitReceipt == nil {
				return false, runtime.endpoints.RecoverCanonical(ctx, s.key, bg.AttemptID)
			}
			green, err := c.AttemptGreen(ctx, bg)
			if err != nil {
				return false, err
			}
			if green == nil {
				return false, fmt.Errorf("committed attempt %s has no verifiable target", bg.AttemptID)
			}
			blue, err := completion.fencedSource(ctx, bg)
			if err != nil {
				return false, err
			}
			if completionReplicationRemoved(parent, bg) {
				return c.removeReplicationSource(ctx, green)
			}
			ready, err := f.completeCommittedAccess(ctx, parent, bg, s, runtime.endpoints, blue, green)
			if err != nil || !ready {
				return false, err
			}
			if refreshSQL {
				bg.SubscriptionCreationAuthorized = nil
			}
			gone, err := c.removeGreenSubscriptions(ctx, green, bg)
			if err != nil || !gone {
				return false, err
			}
			if blue != nil {
				gone, err = completion.sql.RemoveSourceReplication(ctx, blue, bg)
				if err != nil || !gone {
					return false, err
				}
			}
			return c.removeReplicationSource(ctx, green)
		}
		if bg.Gates != nil && bg.Gates.Cancellation != nil && bg.Gates.Cancellation.CredentialsRemovedAt != nil && (bg.Gates.Cancellation.GreenReplicationRemovedAt == nil || bg.Gates.Cancellation.GreenReplicationProofVersion != greenReplicationProofVersion) && bg.Green != nil {
			return false, fmt.Errorf("attempt %s retained candidate %s lacks SQL subscription-removal proof; inspect and remove its subscriptions and source slots using authorized administrative access; keep the parent finalizer and credentials", bg.AttemptID, bg.Green.Ref.Name)
		}
		if bg.Gates == nil {
			bg.Gates = &platformv1alpha1.BlueGreenGateStatus{}
		}
		if bg.Gates.Cancellation == nil {
			bg.Gates.Cancellation = &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: new(metav1.Now())}
			return false, f.saveFinalizationEvidence(ctx, parent, bg)
		}
		cancellation := bg.Gates.Cancellation
		if cancellation.SourceAccessRestoredAt == nil {
			done, err := s.restoreForCancellationWith(ctx, bg.AttemptID, bg, runtime.endpoints)
			if err != nil || !done {
				return false, err
			}
			cancellation.SourceAccessRestoredAt = new(metav1.Now())
			return false, f.saveFinalizationEvidence(ctx, parent, bg)
		}
		if refreshSQL {
			// An old receipt predates these writers. Re-prove SQL after their acknowledgement.
			bg.SubscriptionCreationAuthorized = nil
			cancellation.GreenReplicationProofVersion = 0
			if _, err := c.ensureGreenReplicationRemoved(ctx, bg); err != nil {
				return false, err
			}
			if cancellation.GreenReplicationProofVersion != greenReplicationProofVersion {
				return false, nil
			}
		}
		done, err := c.RemoveReplication(ctx, bg)
		if err != nil {
			return false, err
		}
		old := entry.BlueGreen.Gates.Cancellation
		if old == nil || !equality.Semantic.DeepEqual(old, bg.Gates.Cancellation) {
			return false, f.saveFinalizationEvidence(ctx, parent, bg)
		}
		if !done {
			return false, nil
		}
	}
	return true, nil
}

func (f *ClusterFinalizer) completeCommittedAccess(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, s *BlueGreenSwitchover, endpoints recoveryEndpoints, blue, green *cnpgv1.Cluster) (bool, error) {
	w := bg.Switchover
	if w == nil || w.ControlIdentity == nil || w.ControlIdentity.PreparedAt == nil || w.FenceVerifiedAt == nil {
		return false, fmt.Errorf("committed finalization requires the recorded access-fence proof")
	}
	if err := endpoints.RecoverCanonical(ctx, s.key, bg.AttemptID); err != nil {
		return false, err
	}
	id, err := s.access.Credentials(ctx, bg)
	if err != nil {
		return false, err
	}
	if blue != nil {
		fenced, err := s.access.FenceBlue(ctx, blue, bg)
		if err != nil || !fenced {
			return false, err
		}
		drained, err := s.sql.DrainRetainedSource(ctx, blue, id, bg)
		if err != nil || !drained {
			return false, err
		}
	}
	if w.BlueSessionsTerminatedAt == nil {
		w.BlueSessionsTerminatedAt = new(metav1.Now())
		return false, f.saveFinalizationEvidence(ctx, parent, bg)
	}
	open, err := s.access.OpenGreen(ctx, green, bg)
	if err != nil || !open {
		return false, err
	}
	if w.GreenFenceRemovedAt == nil {
		w.GreenFenceRemovedAt = new(metav1.Now())
		return false, f.saveFinalizationEvidence(ctx, parent, bg)
	}
	return true, nil
}

func completionReplicationRemoved(parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	c, w := bg.Completion, bg.Switchover
	if c == nil || w == nil || w.BlueSessionsTerminatedAt == nil || w.GreenFenceRemovedAt == nil ||
		c.StartedAt == nil || c.SourceStoppedAt == nil || c.ReplicationRemovedAt == nil ||
		!mvutypes.BlueGreenRecoverable(bg) || mvutypes.CutoverStageOfStatus(bg) != mvutypes.CutoverHandedOff ||
		!pgendpointprotocol.CommitReceiptIdentityMatches(parent, bg, pgendpointprotocol.AllowProviderRepair) {
		return false
	}
	// Completion removed all replication declarations before its SQL checkpoint.
	// A later declaration can have recreated SQL and invalidates that proof.
	return !finalizationRequiresFreshSQL(parent)
}

func finalizationRequiresFreshSQL(parent *platformv1alpha1.PostgresCluster) bool {
	return parent.Status.Finalization != nil && !parent.Status.Finalization.SQLDone && slices.ContainsFunc(parent.Status.Finalization.Resources, func(ref corev1.ObjectReference) bool {
		return ref.Kind == "Subscription" || ref.Kind == "Publication"
	})
}

// SQL receipts do not freeze a surviving source's live access configuration.
func (f *ClusterFinalizer) verifyRetainedFences(ctx context.Context, parent *platformv1alpha1.PostgresCluster) error {
	for _, entry := range parent.Status.PostgresMajorUpgradeStatus {
		if entry.BlueGreen == nil || entry.BlueGreen.CommitReceipt == nil || mvutypes.IsBlueGreenAttemptCleaned(entry) {
			continue
		}
		runtime, err := f.runtime(parent)
		if err != nil {
			return err
		}
		if _, err := runtime.completion.fencedSource(ctx, entry.BlueGreen); err != nil {
			return err
		}
	}
	return nil
}

// Preserve coordinator-owned endpoint fields when recording a finalization checkpoint.
func (f *ClusterFinalizer) saveFinalizationEvidence(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	fresh := &platformv1alpha1.PostgresCluster{}
	if err := f.reader.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
		return err
	}
	if fresh.UID != parent.UID || fresh.DeletionTimestamp == nil || len(fresh.Status.PostgresMajorUpgradeStatus) == 0 {
		return fmt.Errorf("finalization evidence parent changed")
	}
	latest := mvutypes.LatestAttempt(fresh.Status.PostgresMajorUpgradeStatus).BlueGreen
	if latest == nil || latest.AttemptID != bg.AttemptID {
		return fmt.Errorf("finalization evidence attempt changed")
	}
	before := fresh.DeepCopy()
	if latest.Green == nil {
		latest.Green = bg.Green.DeepCopy()
	}
	if latest.Gates == nil {
		latest.Gates = &platformv1alpha1.BlueGreenGateStatus{}
	}
	if bg.Gates != nil {
		latest.Gates.Cancellation = bg.Gates.Cancellation.DeepCopy()
	}
	if latest.Switchover != nil && bg.Switchover != nil {
		latest.Switchover.SourceFenceRestoredAt = bg.Switchover.SourceFenceRestoredAt.DeepCopy()
		latest.Switchover.BlueSessionsTerminatedAt = bg.Switchover.BlueSessionsTerminatedAt.DeepCopy()
		latest.Switchover.GreenFenceRemovedAt = bg.Switchover.GreenFenceRemovedAt.DeepCopy()
	}
	return f.client.Status().Patch(ctx, fresh, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}
