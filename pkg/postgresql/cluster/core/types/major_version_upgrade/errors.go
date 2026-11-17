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

package majorversionupgradetypes

import (
	"errors"
	"fmt"

	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
)

var (
	// Trivial errors are useful for logs or metrics, but should not fail the
	// upgrade workflow.
	ErrUpgradeIntentMissing   = errors.New("major upgrade intent is missing")
	ErrUpgradeAlreadyComplete = errors.New("major upgrade is already complete")
	ErrUpgradeNoop            = errors.New("major upgrade has no work to perform")

	// Retryable errors mean the requested operation is still possible, but the
	// reconciler should requeue and try again.
	ErrStateTemporarilyUnavailable = errors.New("major upgrade state is temporarily unavailable")
	ErrStatusPersistConflict       = errors.New("major upgrade status persist conflict")
	ErrRollbackCapabilityNotReady  = errors.New("major upgrade rollback capability is not ready")
	ErrBackupStatusMissing         = errors.New("major upgrade backup status is missing")
	ErrUpgradeFlowPending          = errors.New("major upgrade flow is pending")
	ErrPreUpgradeBackupNotReady    = errors.New("major upgrade pre-upgrade backup is not ready")
	ErrPostUpgradeBackupNotReady   = errors.New("major upgrade post-upgrade backup is not ready")

	// Terminal errors mean the operator should move the workflow to Failed and
	// surface that external intervention is required.
	ErrInvalidUpgradeIntent               = errors.New("major upgrade intent is invalid")
	ErrUnsupportedUpgradeStrategy         = errors.New("major upgrade strategy is unsupported")
	ErrBlueGreenStrategyUnavailable       = errors.New("blue/green major upgrade strategy is not implemented")
	ErrRollbackCapabilityMissing          = errors.New("major upgrade rollback capability port is not configured")
	ErrBackupProviderMissing              = errors.New("major upgrade backup provider is not configured")
	ErrTargetWALArchiveMissing            = errors.New("blue/green completion requires class-configured Barman WAL archiving; snapshot-only protection cannot establish the target recovery baseline")
	ErrRollbackCapabilityUnavailable      = errors.New("major upgrade rollback capability is unavailable")
	ErrUpgradeFlowFailed                  = errors.New("major upgrade flow failed")
	ErrUpgradeVerificationFailed          = errors.New("major upgrade verification failed")
	ErrUpgradeUnrecoverablePreConversion  = errors.New("major upgrade failed before data directory conversion")
	ErrUpgradeUnrecoverablePostConversion = errors.New("major upgrade failed after data directory conversion")

	// ErrCandidateInvalid is a terminal blue/green failure after which a retry
	// cannot reuse the attempt. Return it only for evidence that invalidates
	// what green was built from or what it holds:
	//   - drift of the frozen baseline: configuration, credentials, CA,
	//     schema and access fingerprints, and the database, role, object,
	//     publication, subscription, slot, and sequence inventories;
	//   - broken logical history: an invalidated slot, a subscription apply
	//     error, or a changed publication, subscription, or replication
	//     credential owned by the attempt;
	//   - green content or identity that no longer matches the attempt: a
	//     mismatched schema import, tampered write guards, or a changed or
	//     replaced environment.
	// Eligibility checks before the baseline is frozen, unavailable
	// dependencies, wiring errors, and failed operations whose inputs remain
	// valid stay ErrUpgradeFlowFailed and may be retried.
	ErrCandidateInvalid = errors.New("blue/green attempt must be rebuilt")
)

// AsCandidateInvalid classifies a terminal failure from a shared check as
// rebuild evidence. Other errors are returned unchanged.
func AsCandidateInvalid(err error) error {
	if err == nil || errors.Is(err, ErrCandidateInvalid) || !IsTerminalFlowFailure(err) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrCandidateInvalid, err)
}

// IsTerminalFlowFailure reports a flow failure that must stop the workflow
// until explicit user action. Classify through it instead of matching
// ErrUpgradeFlowFailed directly, so every terminal flow sentinel is covered.
func IsTerminalFlowFailure(err error) bool {
	return errors.Is(err, ErrUpgradeFlowFailed) || errors.Is(err, ErrCandidateInvalid)
}

func ReportFromError(err error) reconciliationtypes.Report {
	switch {
	case err == nil:
		return reconciliationtypes.Report{}

	case errors.Is(err, ErrUpgradeIntentMissing):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Scheduled),
			Reason: ReasonUpgradeIntentMissing, Message: reportMessageUpgradeIntentMissing,
			Retry: false, Sleep: new(reportSleepNone)}
	case errors.Is(err, ErrUpgradeAlreadyComplete):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Completed),
			Reason: ReasonUpgradeAlreadyComplete, Message: reportMessageUpgradeAlreadyComplete,
			Retry: false, Sleep: new(reportSleepNone)}
	case errors.Is(err, ErrUpgradeNoop):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Scheduled),
			Reason: ReasonUpgradeNoop, Message: reportMessageUpgradeNoop,
			Retry: false, Sleep: new(reportSleepNone)}

	case errors.Is(err, ErrStateTemporarilyUnavailable):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Scheduled),
			Reason: ReasonStateLoadFailed, Message: reportMessageStateTemporarilyUnavailable,
			Retry: true, Sleep: new(reportSleepQuickRetrySeconds)}
	case errors.Is(err, ErrStatusPersistConflict):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Scheduled),
			Reason: ReasonStatusPersistConflict, Message: reportMessageStatusPersistConflict,
			Retry: true, Sleep: new(reportSleepQuickRetrySeconds)}
	case errors.Is(err, ErrPreUpgradeBackupNotReady):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(PreUpgradeBackup),
			Reason: ReasonPreUpgradeBackupNotReady, Message: reportMessagePreUpgradeBackupNotReady,
			Retry: true, Sleep: new(reportSleepRetrySeconds)}
	case errors.Is(err, ErrPostUpgradeBackupNotReady):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(PostUpgradeBackup),
			Reason: ReasonPostUpgradeBackupNotReady, Message: reportMessagePostUpgradeBackupNotReady,
			Retry: true, Sleep: new(reportSleepRetrySeconds)}
	case errors.Is(err, ErrRollbackCapabilityNotReady):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(PreUpgradeBackup),
			Reason: ReasonBackupStatusMissing, Message: reportMessageRollbackCapabilityNotReady,
			Retry: true, Sleep: new(reportSleepRetrySeconds)}
	case errors.Is(err, ErrBackupStatusMissing):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(PreUpgradeBackup),
			Reason: ReasonBackupStatusMissing, Message: reportMessageBackupStatusMissing,
			Retry: true, Sleep: new(reportSleepRetrySeconds)}
	case errors.Is(err, ErrUpgradeFlowPending):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Upgrading),
			Reason: ReasonUpgradeFlowPending, Message: reportMessageUpgradeFlowPending,
			Retry: true, Sleep: new(reportSleepLongRetrySeconds)}

	case errors.Is(err, ErrInvalidUpgradeIntent):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonInvalidUpgradeIntent, Message: reportMessageInvalidUpgradeIntent,
			Retry: false, Sleep: new(reportSleepNone)}
	case errors.Is(err, ErrUnsupportedUpgradeStrategy):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonUnsupportedUpgradeStrategy, Message: reportMessageUnsupportedUpgradeStrategy,
			Retry: false, Sleep: new(reportSleepNone)}
	case errors.Is(err, ErrBlueGreenStrategyUnavailable):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonBlueGreenStrategyUnavailable, Message: reportMessageBlueGreenStrategyUnavailable,
			Retry: false, Sleep: new(reportSleepNone)}
	case errors.Is(err, ErrRollbackCapabilityMissing):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonRollbackCapabilityMissing, Message: reportMessageRollbackCapabilityMissing,
			Retry: false, Sleep: new(reportSleepNone)}
	case errors.Is(err, ErrBackupProviderMissing):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonBackupProviderMissing, Message: reportMessageBackupProviderMissing,
			Retry: false, Sleep: new(reportSleepNone)}
	case errors.Is(err, ErrRollbackCapabilityUnavailable):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonRollbackCapabilityFailed, Message: reportMessageRollbackCapabilityUnavailable,
			Retry: false, Sleep: new(reportSleepNone)}
	case errors.Is(err, ErrCandidateInvalid):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonCandidateRebuildRequired, Message: reportMessageCandidateRebuildRequired,
			Retry: false, Sleep: new(reportSleepNone)}
	case errors.Is(err, ErrUpgradeFlowFailed):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonUpgradeFlowFailed, Message: reportMessageUpgradeFlowFailed,
			Retry: false, Sleep: new(reportSleepNone)}
	case errors.Is(err, ErrUpgradeVerificationFailed):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonUpgradeVerificationFailed, Message: reportMessageUpgradeVerificationFailed,
			Retry: false, Sleep: new(reportSleepNone)}
	case errors.Is(err, ErrUpgradeUnrecoverablePreConversion):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonUpgradeUnrecoverablePreConversion, Message: reportMessageUpgradeUnrecoverablePreConversion,
			Retry: false, Sleep: new(reportSleepNone)}
	case errors.Is(err, ErrUpgradeUnrecoverablePostConversion):
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonUpgradeUnrecoverablePostConversion, Message: reportMessageUpgradeUnrecoverablePostConversion,
			Retry: false, Sleep: new(reportSleepNone)}
	default:
		return reconciliationtypes.Report{
			Name: UseCaseName, Phase: string(Scheduled),
			Reason: ReasonUnknownMajorUpgradeError, Message: reportMessageUnknownMajorUpgradeError,
			Retry: true, Sleep: new(reportSleepRetrySeconds)}
	}
}
