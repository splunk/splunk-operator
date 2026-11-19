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
	"math"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AdvanceCleanup persists authorization and barrier changes before deleting resources.
func (c *BlueGreenCleanup) AdvanceCleanup(ctx context.Context, intent mvutypes.Intent) (bluegreen.Outcome, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	parent, entry, err := c.readAttempt(ctx, intent)
	if err != nil {
		return bluegreen.Outcome{}, err
	}
	status := entry.BlueGreen.DeepCopy()
	outcome := cleanupOutcome(entry, status, "Waiting for retained-environment cleanup.")
	fail := func(err error) (bluegreen.Outcome, error) {
		if status.Cleanup != nil && status.Cleanup.StartedAt != nil {
			status.Cleanup.State = platformv1alpha1.BlueGreenCleanupStateCleanupFailed
			status.Cleanup.Failure = new(err.Error())
		}
		outcome.Report.Reason = "CleanupFailed"
		outcome.Report.Message = err.Error()
		return outcome, err
	}
	if mvutypes.IsBlueGreenAttemptCleaned(entry) {
		outcome.Report.Retry = false
		outcome.Report.Reason = mvutypes.ReasonCleaned
		return outcome, nil
	}
	if !mvutypes.BlueGreenCleanupEligible(entry) {
		return fail(fmt.Errorf("cleanup requires complete production or cancellation receipts"))
	}
	recovered, err := c.discoverCleanupCandidate(ctx, parent, status)
	if err != nil {
		return fail(err)
	}
	if recovered {
		return outcome, nil
	}
	snapshot, err := c.readCleanupSnapshot(ctx, parent, entry)
	if err != nil {
		return fail(err)
	}
	active, retirement, rows, roots := snapshot.active, snapshot.retirement, snapshot.rows, snapshot.roots
	if status.Gates == nil || status.Gates.Cleanup == nil {
		if !mvutypes.BlueGreenCleanupRequestedInSpec(&parent.Spec) || parent.Generation != intent.Generation {
			return fail(fmt.Errorf("cleanup permission changed before consumption"))
		}
		if !sourceReadyForFailover(active) {
			outcome.Report.Reason = mvutypes.ReasonCleanupNotReady
			return outcome, nil
		}
		now := metav1.Now()
		if status.Gates == nil {
			status.Gates = &platformv1alpha1.BlueGreenGateStatus{}
		}
		status.Gates.Cleanup = &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: parent.Generation, ConsumedAt: &now}
		status.Cleanup = &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaning, EnvironmentRef: retirement, RetiredResources: roots, StartedAt: &now, RetirementGeneration: new(int64(1)), RetirementAcknowledgements: rows}
		return outcome, nil
	}
	cleanup := status.Cleanup
	if cleanup == nil || cleanup.RetirementGeneration == nil || !equality.Semantic.DeepEqual(cleanup.EnvironmentRef, retirement) {
		return fail(fmt.Errorf("consumed cleanup has incomplete retirement identity"))
	}
	cleanup.State, cleanup.Failure = platformv1alpha1.BlueGreenCleanupStateCleaning, nil
	if !equality.Semantic.DeepEqual(mvutypes.RetirementRoster(cleanup), rows) {
		if *cleanup.RetirementGeneration == math.MaxInt64 {
			return fail(fmt.Errorf("retirement generation is exhausted"))
		}
		*cleanup.RetirementGeneration++
		cleanup.RetirementAcknowledgements = rows
		cleanup.RetiredResources = roots
		return outcome, nil
	}
	if !equality.Semantic.DeepEqual(cleanup.RetiredResources, roots) {
		cleanup.RetiredResources = roots
		return outcome, nil
	}
	ready, changed, err := c.collectRetirementProofs(ctx, parent, status)
	if err != nil {
		return fail(err)
	}
	if !ready || changed {
		return outcome, nil
	}
	verify := func(ctx context.Context) error { return c.verifyRequest(ctx, intent, entry) }
	if err := verify(ctx); err != nil {
		return fail(err)
	}
	done, err := c.retireResources(ctx, parent, status, active, verify)
	if err != nil {
		return fail(err)
	}
	if !done {
		return outcome, nil
	}
	if err := verify(ctx); err != nil {
		return fail(err)
	}
	cleanup.State = platformv1alpha1.BlueGreenCleanupStateCleaned
	cleanup.CompletedAt = new(metav1.Now())
	outcome.Report.Reason = mvutypes.ReasonCleaned
	outcome.Report.Message = "Retained environment cleanup is verified."
	outcome.Report.Retry = false
	return outcome, nil
}

func cleanupOutcome(entry platformv1alpha1.PostgresMajorUpgradeStatus, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, message string) bluegreen.Outcome {
	outcome := bluegreen.Outcome{BlueGreen: status}
	outcome.Report.Name = mvutypes.UseCaseName
	outcome.Report.Phase = *entry.Phase
	outcome.Report.Reason = "EnvironmentRetirement"
	outcome.Report.Message = message
	outcome.Report.Retry = true
	outcome.Report.Sleep = new(1)
	return outcome
}

func (c *BlueGreenCleanup) readAttempt(ctx context.Context, intent mvutypes.Intent) (*platformv1alpha1.PostgresCluster, platformv1alpha1.PostgresMajorUpgradeStatus, error) {
	parent := &platformv1alpha1.PostgresCluster{}
	if err := c.reader.Get(ctx, c.key, parent); err != nil {
		return nil, platformv1alpha1.PostgresMajorUpgradeStatus{}, err
	}
	if parent.UID != c.ownerUID || parent.DeletionTimestamp != nil || len(parent.Status.PostgresMajorUpgradeStatus) == 0 {
		return nil, platformv1alpha1.PostgresMajorUpgradeStatus{}, fmt.Errorf("cleanup parent identity changed")
	}
	entry := *mvutypes.LatestAttempt(parent.Status.PostgresMajorUpgradeStatus)
	if !mvutypes.MatchesIntent(entry, intent) || entry.Phase == nil {
		return nil, entry, fmt.Errorf("cleanup attempt was superseded")
	}
	return parent, entry, nil
}

// discoverCleanupCandidate recovers a creation whose status write was lost.
func (c *BlueGreenCleanup) discoverCleanupCandidate(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	candidates, err := discoverAttemptCandidates(ctx, c.reader, parent, bg)
	if err != nil {
		return false, err
	}
	var candidate *cnpgv1.Cluster
	for i := range candidates {
		ref := &candidates[i]
		if parent.Status.ProvisionerRef != nil && ref.UID == parent.Status.ProvisionerRef.UID || bg.Green != nil && ref.UID == bg.Green.Ref.UID {
			continue
		}
		if candidate != nil || bg.Green != nil || bg.CommitReceipt != nil || bg.Cleanup != nil && bg.Cleanup.StartedAt != nil {
			return false, fmt.Errorf("cleanup found an unexpected attempt-owned environment %s", ref.Name)
		}
		candidate = ref
	}
	if candidate == nil {
		return false, nil
	}
	ref, err := resourceReference(candidate)
	if err != nil {
		return false, err
	}
	bg.Green = &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: ref}
	return true, nil
}

// collectRetirementProofs independently verifies current children and provider absence.
func (c *BlueGreenCleanup) collectRetirementProofs(ctx context.Context, parent *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, bool, error) {
	environment := bg.Cleanup.EnvironmentRef
	if environment != nil {
		var declarations cnpgv1.DatabaseList
		if err := c.reader.List(ctx, &declarations, client.InNamespace(environment.Namespace)); err != nil {
			return false, false, err
		}
		present := map[string]types.UID{}
		remaining := false
		for _, database := range declarations.Items {
			present[database.Name] = database.UID
			remaining = remaining || database.Spec.ClusterRef.Name == environment.Name
		}
		for _, row := range bg.Cleanup.RetirementAcknowledgements {
			for _, ref := range row.Resources {
				if uid, exists := present[ref.Name]; exists {
					if uid != ref.UID {
						return false, false, fmt.Errorf("retired Database %s was replaced", ref.Name)
					}
					remaining = true
				}
			}
		}
		if remaining {
			return false, false, nil
		}
	}
	ready, changed := true, false
	for i := range bg.Cleanup.RetirementAcknowledgements {
		row := &bg.Cleanup.RetirementAcknowledgements[i]
		child := &platformv1alpha1.PostgresDatabase{}
		err := c.reader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: row.PostgresDatabaseName}, child)
		if err != nil && !apierrors.IsNotFound(err) {
			return false, false, err
		}
		if apierrors.IsNotFound(err) || string(child.UID) != row.PostgresDatabaseUID {
			if row.PostgresDatabaseAbsentAt == nil {
				row.PostgresDatabaseAbsentAt = new(metav1.Now())
				changed = true
			}
			continue
		}
		if !mvutypes.RetirementAcknowledged(parent.UID, bg.AttemptID, bg.Cleanup, *row, child) {
			ready = false
			continue
		}
		if row.AcknowledgedAt == nil {
			row.AcknowledgedAt = new(metav1.Now())
			changed = true
		}
	}
	return ready, changed, nil
}

// verifyRequest closes stale-generation and authority windows before each mutation.
func (c *BlueGreenCleanup) verifyRequest(ctx context.Context, intent mvutypes.Intent, expected platformv1alpha1.PostgresMajorUpgradeStatus) error {
	parent, current, err := c.readAttempt(ctx, intent)
	if err != nil {
		return err
	}
	bg := current.BlueGreen
	if !mvutypes.BlueGreenCleanupEligible(current) || !equality.Semantic.DeepEqual(bg.Cleanup, expected.BlueGreen.Cleanup) || bg.Gates == nil || !equality.Semantic.DeepEqual(bg.Gates.Cleanup, expected.BlueGreen.Gates.Cleanup) {
		return fmt.Errorf("cleanup intent changed before mutation")
	}
	if _, err := c.discoverCleanupCandidate(ctx, parent, bg.DeepCopy()); err != nil {
		return err
	}
	snapshot, err := c.readCleanupSnapshot(ctx, parent, current)
	if err != nil {
		return err
	}
	roots := snapshot.roots
	if !equality.Semantic.DeepEqual(roots, bg.Cleanup.RetiredResources) {
		return fmt.Errorf("retirement inventory changed before mutation")
	}
	for _, ref := range roots {
		object, err := loadExactRetirementRoot(ctx, c.reader, parent.Namespace, ref)
		if err != nil {
			return err
		}
		if object == nil {
			continue
		}
		if err := validateCleanupRoot(parent, bg, snapshot.active, snapshot.protected, object); err != nil {
			return err
		}
	}
	if !equality.Semantic.DeepEqual(mvutypes.RetirementRoster(bg.Cleanup), snapshot.rows) {
		return fmt.Errorf("retirement participants changed before mutation")
	}
	ready, _, err := c.collectRetirementProofs(ctx, parent, bg.DeepCopy())
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("retirement proofs no longer verify")
	}
	return nil
}

type cleanupSnapshot struct {
	active     *cnpgv1.Cluster
	retirement *corev1.ObjectReference
	rows       []platformv1alpha1.BlueGreenRetirementAcknowledgementStatus
	roots      []corev1.ObjectReference
	protected  map[string]bool
}

// readCleanupSnapshot is shared by planning and each fresh pre-mutation check.
func (c *BlueGreenCleanup) readCleanupSnapshot(ctx context.Context, parent *platformv1alpha1.PostgresCluster, entry platformv1alpha1.PostgresMajorUpgradeStatus) (cleanupSnapshot, error) {
	var snapshot cleanupSnapshot
	authority, retirement, err := mvutypes.BlueGreenRetirementEnvironments(entry)
	if err != nil {
		return snapshot, err
	}
	if authority == nil {
		return snapshot, fmt.Errorf("cleanup requires the authoritative environment identity")
	}
	snapshot.retirement = retirement
	snapshot.active, _, err = pgcnpg.ObserveRetirementEnvironments(ctx, c.reader, parent, *authority, retirement)
	if err == nil {
		snapshot.rows, err = cleanupInventory(ctx, c.reader, parent, entry.BlueGreen, retirement)
	}
	if err == nil {
		snapshot.protected, err = publishedCleanupSecrets(ctx, c.reader, parent)
	}
	if err == nil {
		snapshot.roots, err = cleanupRoots(ctx, c.reader, parent, entry.BlueGreen, snapshot.active, retirement, snapshot.protected)
	}
	return snapshot, err
}
