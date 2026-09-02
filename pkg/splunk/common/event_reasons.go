// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package common

// EventReason is a PascalCase, machine-readable identifier used as both a
// Kubernetes event reason and a terminal-error reason in CR status conditions.
// Use one of the predefined EventReasonXxx constants rather than a bare string
// literal so that event and condition reasons stay consistent across controllers.
type EventReason = string

const (
	// Normal event reasons
	EventReasonScaledUp              EventReason = "ScaledUp"
	EventReasonScaledDown            EventReason = "ScaledDown"
	EventReasonClusterInitialized    EventReason = "ClusterInitialized"
	EventReasonClusterQuorumLost     EventReason = "ClusterQuorumLost"
	EventReasonClusterQuorumRestored EventReason = "ClusterQuorumRestored"
	EventReasonPasswordSyncCompleted EventReason = "PasswordSyncCompleted"

	// Warning event reasons — spec & config
	EventReasonValidateSpecFailed      EventReason = "ValidateSpecFailed"
	EventReasonApplySplunkConfigFailed EventReason = "ApplySplunkConfigFailed"
	EventReasonAppFrameworkInitFailed  EventReason = "AppFrameworkInitFailed"
	EventReasonAppRepoConnFailed       EventReason = "AppRepositoryConnectionFailed"
	EventReasonSmartStoreConfigPending EventReason = "SmartStoreConfigPending"

	// Warning event reasons — services & statefulsets
	EventReasonApplyServiceFailed      EventReason = "ApplyServiceFailed"
	EventReasonStatefulSetFailed       EventReason = "StatefulSetFailed"
	EventReasonStatefulSetUpdateFailed EventReason = "StatefulSetUpdateFailed"
	EventReasonStatefulSetDeleteFailed EventReason = "StatefulSetDeleteFailed"
	EventReasonOwnerRefFailed          EventReason = "OwnerRefFailed"

	// Warning event reasons — deletion
	EventReasonDeleteFailed EventReason = "DeleteFailed"

	// Warning event reasons — secrets & credentials
	EventReasonSecretMissing        EventReason = "SecretMissing"
	EventReasonSecretInvalid        EventReason = "SecretInvalid"
	EventReasonSecretAccessFailed   EventReason = "SecretAccessFailed"
	EventReasonPasswordSyncFailed   EventReason = "PasswordSyncFailed"
	EventReasonCertSecretMalformed  EventReason = "CertSecretMalformed"
	EventReasonCertSecretWrongOwner EventReason = "CertSecretWrongOwner"

	// Warning event reasons — monitoring console
	EventReasonMonitoringConsoleCleanupFailed EventReason = "MonitoringConsoleCleanupFailed"
	EventReasonMonitoringConsoleConfigFailed  EventReason = "MonitoringConsoleConfigFailed"
	EventReasonMonitoringConsoleRefFailed     EventReason = "MonitoringConsoleRefFailed"
	EventReasonMonitoringConsoleApplyFailed   EventReason = "MonitoringConsoleApplyFailed"
	EventReasonAnnotationUpdateFailed         EventReason = "AnnotationUpdateFailed"
	EventReasonImageGetFailed                 EventReason = "ImageGetFailed"

	// Warning event reasons — cluster operations
	EventReasonResolveQueueObjectStorageFailed EventReason = "ResolveQueueObjectStorageFailed"
	EventReasonImmutableRefsModified           EventReason = "ImmutableRefsModified"
	EventReasonEmptyClusterManagerRef          EventReason = "EmptyClusterManagerRef"
	EventReasonRemoteVolumeKeyCheckFailed      EventReason = "RemoteVolumeKeyCheckFailed"
	EventReasonVerifyRFPeersFailed             EventReason = "VerifyRFPeersFailed"
	EventReasonMaintenanceModeFailed           EventReason = "MaintenanceModeFailed"
	EventReasonRetrieveCMSpecFailed            EventReason = "RetrieveCMSpecFailed"
	EventReasonConfFileUpdateFailed            EventReason = "ConfFileUpdateFailed"
	EventReasonBundlePushFailed                EventReason = "BundlePushFailed"
	EventReasonPodExecFailed                   EventReason = "PodExecFailed"
	EventReasonScalingBlockedRF                EventReason = "ScalingBlockedRF"
	EventReasonLicenseExpired                  EventReason = "LicenseExpired"

	// Warning event reasons — upgrade
	EventReasonUpgradeCheckFailed            EventReason = "UpgradeCheckFailed"
	EventReasonUpgradeBlockedVersionMismatch EventReason = "UpgradeBlockedVersionMismatch"
	EventReasonDetentionTimeoutForced        EventReason = "DetentionTimeoutForced"

	// Stalled condition transition events
	EventReasonStalled         EventReason = "Stalled"
	EventReasonStalledResolved EventReason = "StalledResolved"
)
