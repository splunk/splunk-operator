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

const (
	MajorUpgradeFlowPgUpgrade string = "pgUpgrade"
)

const (
	ReasonBackupStatusAvailable              = "BackupStatusAvailable"
	ReasonBackupStatusMissing                = "BackupStatusMissing"
	ReasonInvalidUpgradeIntent               = "InvalidUpgradeIntent"
	ReasonPgUpgradeFinalized                 = "PgUpgradeFinalized"
	ReasonPgUpgradeObservedComplete          = "PgUpgradeObservedComplete"
	ReasonPgUpgradeStarted                   = "PgUpgradeStarted"
	ReasonPreUpgradeBackupNotReady           = "PreUpgradeBackupBaselineNotReady"
	ReasonPostUpgradeBackupNotReady          = "PostUpgradeBackupBaselineNotReady"
	ReasonPreflightCheckPassed               = "PreflightCheckPassed"
	ReasonRollbackCapabilityMissing          = "RollbackCapabilityPortMissing"
	ReasonBackupProviderMissing              = "BackupProviderMissing"
	ReasonRollbackCapabilityFailed           = "RollbackCapabilityFailed"
	ReasonStateLoadFailed                    = "MajorUpgradeStateLoadFailed"
	ReasonStatusPersistConflict              = "StatusPersistConflict"
	ReasonUnsupportedUpgradeStrategy         = "UnsupportedUpgradeStrategy"
	ReasonUnknownMajorUpgradeError           = "UnknownMajorUpgradeError"
	ReasonUpgradeAlreadyComplete             = "UpgradeAlreadyComplete"
	ReasonUpgradeFlowFailed                  = "UpgradeFlowFailed"
	ReasonUpgradeFlowPending                 = "UpgradeFlowPending"
	ReasonUpgradeFlowReturnedEmptyReport     = "UpgradeFlowReturnedEmptyReport"
	ReasonUpgradeIntentMissing               = "UpgradeIntentMissing"
	ReasonUpgradeNoop                        = "UpgradeNoop"
	ReasonUpgradeVerificationFailed          = "UpgradeVerificationFailed"
	ReasonUpgradeUnrecoverablePreConversion  = "UpgradeUnrecoverablePreConversion"
	ReasonUpgradeUnrecoverablePostConversion = "UpgradeUnrecoverablePostConversion"
)

const (
	UseCaseName = "MajorVersionUpgrade"
)

const (
	AnnotationMajorUpgradeRetryAt = "platform.splunk.com/major-upgrade-retry-at"
)

const (
	ConditionMajorUpgradeProgressing      = "MajorUpgradeProgressing"
	ConditionMajorUpgradeRetryableFailure = "MajorUpgradeRetryableFailure"
	ConditionMajorUpgradeTerminalFailure  = "MajorUpgradeTerminalFailure"
	ConditionMajorUpgradeCompleted        = "MajorUpgradeCompleted"
)

const (
	reportSleepNone              = 0
	reportSleepQuickRetrySeconds = 5
	reportSleepRetrySeconds      = 30
	reportSleepLongRetrySeconds  = 60
)

const (
	MessagePreflightCheckPassed         = "The PostgreSQL major upgrade preflight checks passed."
	MessagePgUpgradeStarted             = "The PostgreSQL pg_upgrade flow has started."
	MessagePgUpgradeStartPending        = "The PostgreSQL pg_upgrade flow has not started yet. The operator will retry."
	MessagePgUpgradeObservedComplete    = "The PostgreSQL pg_upgrade flow completed and is ready for verification."
	MessagePgUpgradeVerificationPending = "The PostgreSQL pg_upgrade conversion is complete; waiting for the CNPG Cluster to return to Ready."
	MessagePgUpgradeStillRunning        = "The PostgreSQL pg_upgrade flow is still running."
	MessagePgUpgradeFinalized           = "The PostgreSQL major upgrade completed verification and is ready for the post-upgrade backup."
)

const (
	reportMessageUpgradeIntentMissing               = "No PostgreSQL major upgrade intent is present."
	reportMessageUpgradeAlreadyComplete             = "The PostgreSQL major upgrade workflow has already completed."
	reportMessageUpgradeNoop                        = "The PostgreSQL major upgrade has no work to perform."
	reportMessageStateTemporarilyUnavailable        = "The operator could not read the major upgrade status. It will retry."
	reportMessageStatusPersistConflict              = "The operator could not persist major upgrade status because the resource changed concurrently. It will retry."
	reportMessageRollbackCapabilityNotReady         = "Waiting for rollback capability before starting the PostgreSQL major upgrade."
	reportMessageBackupStatusMissing                = "Waiting for backup status before starting the PostgreSQL major upgrade."
	reportMessageUpgradeFlowPending                 = "The PostgreSQL major upgrade is still in progress."
	reportMessagePreUpgradeBackupNotReady           = "Waiting for pre-upgrade backup to become available."
	reportMessagePostUpgradeBackupNotReady          = "Waiting for post-upgrade backup to become available."
	reportMessageInvalidUpgradeIntent               = "The PostgreSQL major upgrade request is invalid. Update the upgrade intent before retrying."
	reportMessageUnsupportedUpgradeStrategy         = "The requested PostgreSQL major upgrade strategy is not supported."
	reportMessageRollbackCapabilityMissing          = "The operator is not configured with rollback capability for PostgreSQL major upgrades."
	reportMessageBackupProviderMissing              = "Major upgrades require an enabled volumeSnapshot or barmanObjectStore backup provider."
	reportMessageRollbackCapabilityUnavailable      = "Rollback capability could not be established. Manual intervention is required before upgrade can continue."
	reportMessageUpgradeFlowFailed                  = "The PostgreSQL major upgrade failed while executing the upgrade flow. Manual intervention is required."
	reportMessageUpgradeVerificationFailed          = "The PostgreSQL major upgrade verification failed. Manual intervention is required."
	reportMessageUnknownMajorUpgradeError           = "The PostgreSQL major upgrade encountered an unexpected error. The operator will retry."
	reportMessageUpgradeUnrecoverablePreConversion  = "pg_upgrade failed before the data directory was converted. The old PostgreSQL version may still be able to start. Restore from the pre-upgrade backup recorded in status.postgresMajorUpgradeStatus[].backupNames.preUpgrade to recover."
	reportMessageUpgradeUnrecoverablePostConversion = "pg_upgrade failed after the data directory was converted to the new major version. PGDATA is no longer readable by the old binary. Restore from the pre-upgrade backup recorded in status.postgresMajorUpgradeStatus[].backupNames.preUpgrade to recover."
)
