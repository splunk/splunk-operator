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

// Package bluegreen owns the executable preparation stages of a blue/green
// major upgrade. Each stage has its own port so completed source preparation
// cannot be replayed while a candidate is being reconciled.
package bluegreen

import (
	"context"
	"errors"
	"fmt"
	"math"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sptr "k8s.io/utils/ptr"
)

// Preparer proves the source-side blue/green invariants and reconciles only
// attempt-scoped source artifacts. It must never create or expose green.
type Preparer interface {
	Prepare(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error)
}

// CandidatePreparer owns green-only resources. It must never mutate blue or
// publish ordinary application endpoints/status.
type CandidatePreparer interface {
	PrepareCandidate(context.Context, mvutypes.Intent) (*platformv1alpha1.PostgresBlueGreenUpgradeStatus, error)
}

// Switchover advances a Candidate Ready attempt through its guarded production
// handoff. It is invoked only after candidate readiness has been verified.
type Switchover interface {
	AdvanceSwitchover(context.Context, mvutypes.Intent, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (Outcome, error)
}

// Outcome is the single durable result of one source-preparation transition.
type Outcome struct {
	Report    reconciliationTypes.Report
	BlueGreen *platformv1alpha1.PostgresBlueGreenUpgradeStatus
}

// Flow advances one durable source-preparation step per reconcile.
type Flow struct {
	phase      mvutypes.Status
	preparer   Preparer
	candidate  CandidatePreparer
	switchover Switchover
}

func New(phase mvutypes.Status, preparer Preparer, candidate CandidatePreparer, switchover ...Switchover) Flow {
	flow := Flow{phase: phase, preparer: preparer, candidate: candidate}
	if len(switchover) > 0 {
		flow.switchover = switchover[0]
	}
	return flow
}

// Advance records entry into Preflight before performing external preparation.
// A later reconciliation runs the preflight port; this makes the backup,
// phase transition, and source mutation independently recoverable.
func (f Flow) Advance(ctx context.Context, intent mvutypes.Intent) (Outcome, error) {
	outcome, err := f.advance(ctx, intent)
	if outcome.BlueGreen == nil {
		outcome.BlueGreen = latestBlueGreenStatus(intent)
	}
	if err != nil && outcome.Report.Name == "" {
		outcome.Report = mvutypes.ReportFromError(err)
	}
	progress := mvutypes.NormalizeCutoverProgress(mvutypes.Progress{Report: outcome.Report, BlueGreen: outcome.BlueGreen})
	outcome.Report = progress.Report

	if outcome.Report.Retry && outcome.Report.Sleep == nil {
		// Normal workflow polling must reset the controller's failure backoff.
		// Requeue=true rate-limits every successful wait and can postpone the
		// next readiness check for minutes after a transient startup error.
		outcome.Report.Sleep = new(1)
	}
	return outcome, err
}

func (f Flow) advance(ctx context.Context, intent mvutypes.Intent) (Outcome, error) {
	if intent.AttemptID == "" {
		return Outcome{}, errors.Join(mvutypes.ErrInvalidUpgradeIntent, errors.New("blue/green attempt ID is required"))
	}
	status := latestBlueGreenStatus(intent)
	if mvutypes.CutoverStageOfStatus(status).OwnsRecovery() {
		return f.advanceSwitchover(ctx, intent, status)
	}
	if status != nil && status.Switchover != nil && status.Switchover.SourceFenceRestoredAt != nil {
		if status.Gates == nil || status.Gates.Switchover == nil || status.Gates.Switchover.ConsumedAt == nil {
			return Outcome{BlueGreen: status}, errors.Join(mvutypes.ErrInvalidUpgradeIntent, errors.New("a restored interruption window requires its original consumed switchover gate"))
		}
		if intent.RetryRequestedAt == nil || !intent.RetryRequestedAt.After(status.Switchover.SourceFenceRestoredAt.Time) ||
			status.Switchover.RetryRequestedAt != nil && !intent.RetryRequestedAt.After(status.Switchover.RetryRequestedAt.Time) {
			return Outcome{BlueGreen: status, Report: reconciliationTypes.Report{
				Name: mvutypes.UseCaseName, Phase: string(mvutypes.Failed), Reason: "SwitchoverFailed",
				Message: "Blue access is restored; another interruption requires a newer explicit retry timestamp.",
			}}, nil
		}
		// Green remains fenced and may no longer have candidate DML guards.
		// Ordinary candidate preparation uses pre-fence credentials and cannot
		// safely be replayed here. The runtime verifies the frozen catalogs and
		// current logical stream through the attempt control identity again.
		return beginSwitchover(status, intent)
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
	if SourcePreparationComplete(intent) {
		return f.prepareCandidate(ctx, intent)
	}
	if f.phase != mvutypes.Preflight {
		return Outcome{}, errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("blue/green source preparation cannot resume from phase "+string(f.phase)))
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

func (f Flow) prepareCandidate(ctx context.Context, intent mvutypes.Intent) (Outcome, error) {
	if f.phase != mvutypes.Preflight && f.phase != mvutypes.Upgrading && f.phase != mvutypes.ReadyForSwitchover {
		return Outcome{}, errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("blue/green candidate preparation cannot resume from phase "+string(f.phase)))
	}
	if f.candidate == nil {
		return Outcome{}, errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("blue/green candidate preparer is not configured"))
	}
	status, err := f.candidate.PrepareCandidate(ctx, intent)
	if err != nil {
		if sourcePreparationRetryable(err) {
			return candidatePendingOutcome(status), nil
		}
		if !errors.Is(err, mvutypes.ErrUpgradeFlowFailed) && !errors.Is(err, mvutypes.ErrInvalidUpgradeIntent) {
			return candidateRetryableFailureOutcome(status, err), err
		}
		return Outcome{BlueGreen: status}, err
	}
	if CandidateReady(status) {
		if intent.SwitchoverAuthorized {
			if f.switchover == nil {
				return Outcome{BlueGreen: status}, errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("blue/green switchover port is not configured"))
			}
			return beginSwitchover(status, intent)
		}
		return candidateReadyOutcome(status), nil
	}
	return candidatePendingOutcome(status), nil
}

// beginSwitchover durably consumes the gate before the runtime prepares its
// restricted control access. The runtime latches the timeout immediately before
// declaring the fence so control bootstrap does not consume the interruption.
func beginSwitchover(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, intent mvutypes.Intent) (Outcome, error) {
	if intent.Generation <= 0 {
		return Outcome{}, errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("switchover gate requires a positive PostgresCluster generation"))
	}
	status = status.DeepCopy()
	if previous := status.Switchover; previous != nil && previous.SourceFenceRestoredAt != nil {
		if status.CommitReceipt != nil || intent.RetryRequestedAt == nil ||
			!intent.RetryRequestedAt.After(previous.SourceFenceRestoredAt.Time) ||
			previous.RetryRequestedAt != nil && !intent.RetryRequestedAt.After(previous.RetryRequestedAt.Time) {
			return Outcome{BlueGreen: status}, errors.Join(mvutypes.ErrInvalidUpgradeIntent, errors.New("another switchover requires verified blue restoration and a newer explicit retry request"))
		}
		if previous.Sequence == math.MaxInt64 {
			return Outcome{BlueGreen: status}, errors.Join(mvutypes.ErrInvalidUpgradeIntent, errors.New("switchover sequence overflow"))
		}
		// The attempt's identities and frozen restoration snapshots survive a
		// retry. Every interruption-specific proof must be established again.
		status.Switchover.BlueGreenSwitchoverWindow = platformv1alpha1.BlueGreenSwitchoverWindow{
			Sequence: previous.Sequence + 1, RetryRequestedAt: intent.RetryRequestedAt.DeepCopy(),
		}
	}
	if status.Gates == nil {
		status.Gates = &platformv1alpha1.BlueGreenGateStatus{}
	}
	if status.Gates.Switchover == nil {
		status.Gates.Switchover = &platformv1alpha1.BlueGreenGateConsumptionStatus{
			Generation: intent.Generation,
			ConsumedAt: k8sptr.To(metav1.Now()),
		}
	}
	if status.Switchover == nil {
		status.Switchover = &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, RetryRequestedAt: intent.RetryRequestedAt.DeepCopy()}
	}
	return Outcome{
		Report: reconciliationTypes.Report{
			Name: mvutypes.UseCaseName, Phase: string(mvutypes.SwitchingOver),
			Reason:  mvutypes.ReasonUpgradeFlowPending,
			Message: "The switchover gate is consumed; preparing restricted control access before fencing.", Retry: true,
		},
		BlueGreen: status,
	}, nil
}

func (f Flow) advanceSwitchover(ctx context.Context, intent mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (Outcome, error) {
	if f.switchover == nil {
		return Outcome{}, errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("blue/green switchover port is not configured"))
	}
	return f.switchover.AdvanceSwitchover(ctx, intent, status)
}

func latestBlueGreenStatus(intent mvutypes.Intent) *platformv1alpha1.PostgresBlueGreenUpgradeStatus {
	for i := len(intent.State) - 1; i >= 0; i-- {
		entry := intent.State[i]
		if mvutypes.MatchesIntent(entry, intent) && entry.BlueGreen != nil {
			return entry.BlueGreen.DeepCopy()
		}
	}
	return nil
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

func candidatePendingOutcome(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) Outcome {
	return Outcome{Report: reconciliationTypes.Report{
		Name:    mvutypes.UseCaseName,
		Phase:   string(mvutypes.Upgrading),
		Reason:  mvutypes.ReasonUpgradeFlowPending,
		Message: "Waiting for the target-major green candidate to be prepared.",
		Retry:   true,
	}, BlueGreen: status}
}

func candidateReadyOutcome(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) Outcome {
	return Outcome{Report: reconciliationTypes.Report{
		Name:    mvutypes.UseCaseName,
		Phase:   string(mvutypes.ReadyForSwitchover),
		Reason:  mvutypes.ReasonCandidateReady,
		Message: mvutypes.MessageCandidateReady,
		// Candidate Ready is a continuously verified state, not a terminal
		// workflow result. A later invariant failure must still be observed.
		Retry: true,
	}, BlueGreen: status}
}

// CandidateReady is deliberately structural: adapters prove individual
// catalog/provider facts and persist them under the durable attempt status;
// core alone decides when that evidence is enough to certify the candidate.
func CandidateReady(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	if status == nil || status.Green == nil || status.Green.AvailableAt == nil || status.CandidateEndpoints == nil || status.CandidateEndpoints.DirectService == nil ||
		status.SourceConfigurationFingerprint == nil || len(status.CredentialFingerprints) == 0 || len(status.DatabaseReplication) == 0 {
		return false
	}
	for _, database := range status.DatabaseReplication {
		if database.DatabaseName == "" || database.SourceSchemaFingerprint == nil || database.SourceAccessPolicyFingerprint == nil ||
			database.TargetSchemaFingerprint == nil || database.TargetAccessPolicyFingerprint == nil ||
			database.SchemaImported == nil || !*database.SchemaImported ||
			database.InitialCopyComplete == nil || !*database.InitialCopyComplete ||
			database.SubscriptionHealthy == nil || !*database.SubscriptionHealthy ||
			database.SlotHealthy == nil || !*database.SlotHealthy ||
			len(database.ReplicaSlots) == 0 {
			return false
		}
		for _, slot := range database.ReplicaSlots {
			if slot.Valid == nil || !*slot.Valid {
				return false
			}
		}
	}
	return true
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

func candidateRetryableFailureOutcome(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, err error) Outcome {
	return Outcome{Report: reconciliationTypes.Report{
		Name:    mvutypes.UseCaseName,
		Phase:   string(mvutypes.Upgrading),
		Reason:  mvutypes.ReasonUnknownMajorUpgradeError,
		Message: fmt.Sprintf("Green candidate preparation failed and will be retried: %v", err),
		Retry:   true,
	}, BlueGreen: status}
}

// sourcePreparationComplete identifies the immutable receipt produced by the
// source-only stage. The parent phase and its generic progressing condition
// necessarily change as green advances, so neither is evidence that source
// preparation may be replayed.
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
