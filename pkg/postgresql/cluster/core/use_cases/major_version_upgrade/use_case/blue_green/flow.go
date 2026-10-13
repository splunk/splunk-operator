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

// Package bluegreen owns the executable source-preparation portion of a
// blue/green major upgrade. It deliberately has no target, subscription,
// endpoint, fencing, or cleanup dependencies; those belong to later workflow
// stages and tickets.
package bluegreen

import (
	"context"
	"errors"
	"fmt"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
)

// Preparer proves the source-side blue/green invariants and reconciles only
// attempt-scoped source artifacts. It must never create or expose green.
type Preparer interface {
	Prepare(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error)
}

// Outcome is the single durable result of one source-preparation transition.
type Outcome struct {
	Report    reconciliationTypes.Report
	BlueGreen *platformv1alpha1.PostgresBlueGreenUpgradeStatus
}

// Flow advances one durable source-preparation step per reconcile.
type Flow struct {
	phase    mvutypes.Status
	preparer Preparer
}

func New(phase mvutypes.Status, preparer Preparer) Flow {
	return Flow{phase: phase, preparer: preparer}
}

// Advance records entry into Preflight before performing external preparation.
// A later reconciliation runs the preflight port; this makes the backup,
// phase transition, and source mutation independently recoverable.
func (f Flow) Advance(ctx context.Context, intent mvutypes.Intent) (Outcome, error) {
	if intent.AttemptID == "" {
		return Outcome{}, errors.Join(mvutypes.ErrInvalidUpgradeIntent, errors.New("blue/green attempt ID is required"))
	}
	if f.phase == mvutypes.Scheduled || f.phase == mvutypes.PreUpgradeBackup {
		return Outcome{Report: reconciliationTypes.Report{
			Name:    mvutypes.UseCaseName,
			Phase:   string(mvutypes.Preflight),
			Reason:  mvutypes.ReasonUpgradeFlowPending,
			Message: "The blue/green source preflight is ready to run.",
			Retry:   true,
		}}, nil
	}
	if f.phase != mvutypes.Preflight {
		return Outcome{}, errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("blue/green source preparation cannot resume from phase "+string(f.phase)))
	}
	if SourcePreparationComplete(intent) {
		return Outcome{Report: reconciliationTypes.Report{
			Name:    mvutypes.UseCaseName,
			Phase:   string(mvutypes.Preflight),
			Reason:  mvutypes.ReasonPreflightCheckPassed,
			Message: mvutypes.MessagePreflightCheckPassed,
		}}, nil
	}
	if f.preparer == nil {
		return Outcome{}, errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("blue/green source preparer is not configured"))
	}
	status, err := f.preparer.Prepare(ctx, intent)
	if err != nil {
		if sourcePreparationRetryable(err) {
			return pendingOutcome(status), nil
		}
		if !errors.Is(err, mvutypes.ErrUpgradeFlowFailed) && !errors.Is(err, mvutypes.ErrInvalidUpgradeIntent) {
			return retryableFailureOutcome(status, err), err
		}
		return Outcome{BlueGreen: status}, err
	}
	return Outcome{
		Report: reconciliationTypes.Report{
			Name:    mvutypes.UseCaseName,
			Phase:   string(mvutypes.Preflight),
			Reason:  mvutypes.ReasonPreflightCheckPassed,
			Message: mvutypes.MessagePreflightCheckPassed,
			Retry:   true,
		},
		BlueGreen: status,
	}, nil
}

func pendingOutcome(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) Outcome {
	return Outcome{Report: reconciliationTypes.Report{
		Name:    mvutypes.UseCaseName,
		Phase:   string(mvutypes.Preflight),
		Reason:  mvutypes.ReasonUpgradeFlowPending,
		Message: "Waiting for source publication preparation to finish.",
		Retry:   true,
	}, BlueGreen: status}
}

// sourcePreparationRetryable keeps infrastructure observations in the source
// preparation phase. Preparation adapters use ErrUpgradeFlowFailed only for a
// proved invariant violation; all other errors are safe to retry without
// re-entering the generic pg_upgrade error mapping.
func sourcePreparationRetryable(err error) bool {
	return errors.Is(err, mvutypes.ErrUpgradeFlowPending)
}

func retryableFailureOutcome(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, err error) Outcome {
	return Outcome{Report: reconciliationTypes.Report{
		Name:    mvutypes.UseCaseName,
		Phase:   string(mvutypes.Preflight),
		Reason:  mvutypes.ReasonUnknownMajorUpgradeError,
		Message: fmt.Sprintf("Source preparation failed and will be retried: %v", err),
		Retry:   true,
	}, BlueGreen: status}
}

// sourcePreparationComplete identifies the durable receipt produced by this
// source-only stage. Later stages advance from that receipt; source
// preparation must not rotate credentials or rebuild publications after it
// exists.
func SourcePreparationComplete(intent mvutypes.Intent) bool {
	for i := len(intent.State) - 1; i >= 0; i-- {
		entry := intent.State[i]
		if !mvutypes.MatchesIntent(entry, intent) || entry.BlueGreen == nil {
			continue
		}
		if entry.BlueGreen.SourcePreparation == nil || entry.BlueGreen.SourcePreparation.CompletedAt == nil {
			return false
		}
		return true
	}
	return false
}
