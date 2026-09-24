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

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	"k8s.io/utils/ptr"
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
	ErrRollbackCapabilityMissing          = errors.New("major upgrade rollback capability port is not configured")
	ErrBackupProviderMissing              = errors.New("major upgrade backup provider is not configured")
	ErrRollbackCapabilityUnavailable      = errors.New("major upgrade rollback capability is unavailable")
	ErrUpgradeFlowFailed                  = errors.New("major upgrade flow failed")
	ErrUpgradeVerificationFailed          = errors.New("major upgrade verification failed")
	ErrUpgradeUnrecoverablePreConversion  = errors.New("major upgrade failed before data directory conversion")
	ErrUpgradeUnrecoverablePostConversion = errors.New("major upgrade failed after data directory conversion")
)

func ReportFromError(err error) reconciliationTypes.Report {
	switch {
	case err == nil:
		return reconciliationTypes.Report{}

	case errors.Is(err, ErrUpgradeIntentMissing):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Scheduled),
			Reason: ReasonUpgradeIntentMissing, Message: reportMessageUpgradeIntentMissing,
			Retry: false, Sleep: ptr.To(reportSleepNone)}
	case errors.Is(err, ErrUpgradeAlreadyComplete):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Completed),
			Reason: ReasonUpgradeAlreadyComplete, Message: reportMessageUpgradeAlreadyComplete,
			Retry: false, Sleep: ptr.To(reportSleepNone)}
	case errors.Is(err, ErrUpgradeNoop):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Scheduled),
			Reason: ReasonUpgradeNoop, Message: reportMessageUpgradeNoop,
			Retry: false, Sleep: ptr.To(reportSleepNone)}

	case errors.Is(err, ErrStateTemporarilyUnavailable):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Scheduled),
			Reason: ReasonStateLoadFailed, Message: reportMessageStateTemporarilyUnavailable,
			Retry: true, Sleep: ptr.To(reportSleepQuickRetrySeconds)}
	case errors.Is(err, ErrStatusPersistConflict):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Scheduled),
			Reason: ReasonStatusPersistConflict, Message: reportMessageStatusPersistConflict,
			Retry: true, Sleep: ptr.To(reportSleepQuickRetrySeconds)}
	case errors.Is(err, ErrPreUpgradeBackupNotReady):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(PreUpgradeBackup),
			Reason: ReasonPreUpgradeBackupNotReady, Message: reportMessagePreUpgradeBackupNotReady,
			Retry: true, Sleep: ptr.To(reportSleepRetrySeconds)}
	case errors.Is(err, ErrPostUpgradeBackupNotReady):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(PostUpgradeBackup),
			Reason: ReasonPostUpgradeBackupNotReady, Message: reportMessagePostUpgradeBackupNotReady,
			Retry: true, Sleep: ptr.To(reportSleepRetrySeconds)}
	case errors.Is(err, ErrRollbackCapabilityNotReady):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(PreUpgradeBackup),
			Reason: ReasonBackupStatusMissing, Message: reportMessageRollbackCapabilityNotReady,
			Retry: true, Sleep: ptr.To(reportSleepRetrySeconds)}
	case errors.Is(err, ErrBackupStatusMissing):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(PreUpgradeBackup),
			Reason: ReasonBackupStatusMissing, Message: reportMessageBackupStatusMissing,
			Retry: true, Sleep: ptr.To(reportSleepRetrySeconds)}
	case errors.Is(err, ErrUpgradeFlowPending):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Upgrading),
			Reason: ReasonUpgradeFlowPending, Message: reportMessageUpgradeFlowPending,
			Retry: true, Sleep: ptr.To(reportSleepLongRetrySeconds)}

	case errors.Is(err, ErrInvalidUpgradeIntent):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonInvalidUpgradeIntent, Message: reportMessageInvalidUpgradeIntent,
			Retry: false, Sleep: ptr.To(reportSleepNone)}
	case errors.Is(err, ErrUnsupportedUpgradeStrategy):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonUnsupportedUpgradeStrategy, Message: reportMessageUnsupportedUpgradeStrategy,
			Retry: false, Sleep: ptr.To(reportSleepNone)}
	case errors.Is(err, ErrRollbackCapabilityMissing):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonRollbackCapabilityMissing, Message: reportMessageRollbackCapabilityMissing,
			Retry: false, Sleep: ptr.To(reportSleepNone)}
	case errors.Is(err, ErrBackupProviderMissing):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonBackupProviderMissing, Message: reportMessageBackupProviderMissing,
			Retry: false, Sleep: ptr.To(reportSleepNone)}
	case errors.Is(err, ErrRollbackCapabilityUnavailable):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonRollbackCapabilityFailed, Message: reportMessageRollbackCapabilityUnavailable,
			Retry: false, Sleep: ptr.To(reportSleepNone)}
	case errors.Is(err, ErrUpgradeFlowFailed):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonUpgradeFlowFailed, Message: reportMessageUpgradeFlowFailed,
			Retry: false, Sleep: ptr.To(reportSleepNone)}
	case errors.Is(err, ErrUpgradeVerificationFailed):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonUpgradeVerificationFailed, Message: reportMessageUpgradeVerificationFailed,
			Retry: false, Sleep: ptr.To(reportSleepNone)}
	case errors.Is(err, ErrUpgradeUnrecoverablePreConversion):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonUpgradeUnrecoverablePreConversion, Message: reportMessageUpgradeUnrecoverablePreConversion,
			Retry: false, Sleep: ptr.To(reportSleepNone)}
	case errors.Is(err, ErrUpgradeUnrecoverablePostConversion):
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Failed),
			Reason: ReasonUpgradeUnrecoverablePostConversion, Message: reportMessageUpgradeUnrecoverablePostConversion,
			Retry: false, Sleep: ptr.To(reportSleepNone)}
	default:
		return reconciliationTypes.Report{
			Name: UseCaseName, Phase: string(Scheduled),
			Reason: ReasonUnknownMajorUpgradeError, Message: reportMessageUnknownMajorUpgradeError,
			Retry: true, Sleep: ptr.To(reportSleepRetrySeconds)}
	}
}
