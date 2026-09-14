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

	"github.com/google/uuid"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SaveBlueGreenRearm is intentionally separate from ReadMajorUpgradeIntent.
// Re-arm eligibility is discovered during Schedule, but only Act may record
// the durable false-gate observation that enables a later attempt.
func (r *majorUpgradeInfoStoreAdapter) SaveBlueGreenRearm(ctx context.Context, intent mvutypes.Intent) error {
	if !intent.RequiresBlueGreenRearm {
		return fmt.Errorf("blue/green re-arm write requires a re-arm intent")
	}
	next, changed := stateWithBlueGreenRearm(intent.State, intent)
	if !changed {
		return nil
	}
	return r.store.SetMajorUpgradeStatus(ctx, next)
}

// shouldObserveBlueGreenRearm detects the only inactive configuration that
// still has durable workflow meaning. The controller must observe allow=false
// after a cleaned attempt before allow=true can create another attempt for the
// same upgrade family.
func shouldObserveBlueGreenRearm(spec *platformv1alpha1.PostgresClusterSpec) bool {
	if spec == nil || spec.PostgresVersion == nil || spec.PostgresMajorUpgradeConfig == nil {
		return false
	}
	config := spec.PostgresMajorUpgradeConfig
	return majorUpgradeStrategy(config) == mvutypes.MajorUpgradeFlowBlueGreen &&
		config.Allow != nil && !*config.Allow
}

// blueGreenRearmIntent returns a read-only execution directive for the single
// status write that records a false allow gate after a cleaned attempt. It
// deliberately returns the existing attempt ID: the write marks that history
// entry re-armed; a later allow=true selection mints the next attempt ID.
func blueGreenRearmIntent(entries []platformv1alpha1.PostgresMajorUpgradeStatus, source, target string) (mvutypes.Intent, bool, error) {
	if source == "" || samePostgresMajor(source, target) {
		return mvutypes.Intent{}, false, nil
	}
	intent := mvutypes.Intent{
		Strategy:        mvutypes.MajorUpgradeFlowBlueGreen,
		SourcePgVersion: source,
		TargetPgVersion: target,
		Policy:          mvutypes.DefaultUpgradePolicy(),
		State:           append([]platformv1alpha1.PostgresMajorUpgradeStatus(nil), entries...),
	}
	for i := range slices.Backward(entries) {
		entry := entries[i]
		if !mvutypes.MatchesUpgradeFamily(entry, intent) ||
			!mvutypes.IsBlueGreenAttemptCleaned(entry) ||
			entry.BlueGreen.RearmedAt != nil {
			continue
		}
		intent.AttemptID = entry.BlueGreen.AttemptID
		intent.RequiresBlueGreenRearm = true
		return intent, true, nil
	}
	return mvutypes.Intent{}, false, nil
}

func blueGreenAttemptID(intent mvutypes.Intent) (string, bool) {
	for i := range slices.Backward(intent.State) {
		entry := intent.State[i]
		if !mvutypes.IsBlueGreenAttemptActive(entry) {
			continue
		}
		if !mvutypes.MatchesUpgradeFamily(entry, intent) || entry.BlueGreen.AttemptID == "" {
			return "", false
		}
		return entry.BlueGreen.AttemptID, true
	}

	for i := range slices.Backward(intent.State) {
		entry := intent.State[i]
		if !mvutypes.MatchesUpgradeFamily(entry, intent) {
			continue
		}
		if entry.BlueGreen == nil || entry.BlueGreen.AttemptID == "" {
			// A legacy or partial blue/green entry is unsafe to bypass. Do not
			// create an attempt that could obscure retained workflow state.
			return "", false
		}
		if mvutypes.IsBlueGreenAttemptActive(entry) {
			return entry.BlueGreen.AttemptID, true
		}
		if entry.BlueGreen.RearmedAt == nil {
			return "", false
		}
		return uuid.NewString(), true
	}

	return uuid.NewString(), true
}

func stateWithBlueGreenRearm(entries []platformv1alpha1.PostgresMajorUpgradeStatus, intent mvutypes.Intent) ([]platformv1alpha1.PostgresMajorUpgradeStatus, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if !mvutypes.MatchesIntent(entry, intent) {
			continue
		}
		if !mvutypes.IsBlueGreenAttemptCleaned(entry) || entry.BlueGreen.RearmedAt != nil {
			return entries, false
		}

		next := append([]platformv1alpha1.PostgresMajorUpgradeStatus(nil), entries...)
		next[i] = *entry.DeepCopy()
		now := metav1.Now()
		next[i].BlueGreen.RearmedAt = &now
		return next, true
	}
	return entries, false
}

func applyBlueGreenProgress(current *platformv1alpha1.PostgresMajorUpgradeStatus, intent mvutypes.Intent, progress mvutypes.Progress) {
	if intent.Strategy == mvutypes.MajorUpgradeFlowBlueGreen && current.BlueGreen == nil && intent.AttemptID != "" {
		current.BlueGreen = &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: intent.AttemptID}
	}
	if progress.BlueGreen != nil {
		current.BlueGreen = progress.BlueGreen.DeepCopy()
	}
}

// validateBlueGreenProgress protects controller-owned status invariants before
// replacing an attempt record. A writer must never rename an attempt or erase
// or regress its irreversible endpoint-commit receipt.
func validateBlueGreenProgress(intent mvutypes.Intent, progress mvutypes.Progress) error {
	if intent.Strategy != mvutypes.MajorUpgradeFlowBlueGreen {
		return nil
	}
	if intent.AttemptID == "" {
		return fmt.Errorf("blue/green major upgrade progress requires an attempt ID")
	}
	if progress.BlueGreen != nil && progress.BlueGreen.AttemptID != intent.AttemptID {
		return fmt.Errorf("blue/green progress attempt ID %q does not match intent attempt ID %q", progress.BlueGreen.AttemptID, intent.AttemptID)
	}

	for i := len(intent.State) - 1; i >= 0; i-- {
		current := intent.State[i]
		if !mvutypes.MatchesIntent(current, intent) || current.BlueGreen == nil || current.BlueGreen.CommitReceipt == nil {
			continue
		}
		if progress.BlueGreen == nil || progress.BlueGreen.CommitReceipt == nil {
			return fmt.Errorf("blue/green progress cannot remove endpoint commit receipt for attempt %q", intent.AttemptID)
		}
		if progress.BlueGreen.CommitReceipt.Sequence < current.BlueGreen.CommitReceipt.Sequence {
			return fmt.Errorf("blue/green progress cannot regress endpoint commit receipt for attempt %q", intent.AttemptID)
		}
		break
	}
	return nil
}

const maxCleanedBlueGreenHistory = 10

// compactBlueGreenHistory retains only the durable summary required after a
// cleaned attempt and caps those summaries at the ten most recent entries.
// Entries with retained artifacts are never compacted or pruned.
func compactBlueGreenHistory(entries []platformv1alpha1.PostgresMajorUpgradeStatus) []platformv1alpha1.PostgresMajorUpgradeStatus {
	next := append([]platformv1alpha1.PostgresMajorUpgradeStatus(nil), entries...)
	for i := range next {
		if !mvutypes.IsBlueGreenAttemptCleaned(next[i]) {
			continue
		}
		next[i] = compactedBlueGreenHistoryEntry(next[i])
	}

	cleaned := 0
	remove := make(map[int]struct{})
	for i := len(next) - 1; i >= 0; i-- {
		if !mvutypes.IsBlueGreenAttemptCleaned(next[i]) {
			continue
		}
		cleaned++
		if cleaned > maxCleanedBlueGreenHistory {
			remove[i] = struct{}{}
		}
	}
	if len(remove) == 0 {
		return next
	}

	retained := make([]platformv1alpha1.PostgresMajorUpgradeStatus, 0, len(next)-len(remove))
	for i := range next {
		if _, prune := remove[i]; !prune {
			retained = append(retained, next[i])
		}
	}
	return retained
}

func compactedBlueGreenHistoryEntry(entry platformv1alpha1.PostgresMajorUpgradeStatus) platformv1alpha1.PostgresMajorUpgradeStatus {
	current := *entry.DeepCopy()
	blueGreen := current.BlueGreen
	if blueGreen == nil || blueGreen.Cleanup == nil {
		return current
	}
	var failure *string
	if blueGreen.Cleanup.Failure != nil {
		value := *blueGreen.Cleanup.Failure
		failure = &value
	}
	cleanup := &platformv1alpha1.BlueGreenCleanupStatus{
		State:       blueGreen.Cleanup.State,
		StartedAt:   blueGreen.Cleanup.StartedAt.DeepCopy(),
		CompletedAt: blueGreen.Cleanup.CompletedAt.DeepCopy(),
		Failure:     failure,
	}
	current.BlueGreen = &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: blueGreen.AttemptID,
		RearmedAt: blueGreen.RearmedAt.DeepCopy(),
		Cleanup:   cleanup,
	}
	return current
}
