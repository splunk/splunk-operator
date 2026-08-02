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

package enterprise

import (
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
)

const (
	// Normal event reasons
	EventReasonScaledUp                             splcommon.EventReason = "ScaledUp"
	EventReasonScaledDown                           splcommon.EventReason = "ScaledDown"
	EventReasonClusterInitialized                   splcommon.EventReason = "ClusterInitialized"
	EventReasonClusterQuorumLost                    splcommon.EventReason = "ClusterQuorumLost"
	EventReasonClusterQuorumRestored                splcommon.EventReason = "ClusterQuorumRestored"
	EventReasonPasswordSyncCompleted                splcommon.EventReason = "PasswordSyncCompleted"
	EventReasonSHCRolloutTargetStarted              splcommon.EventReason = "SHCRolloutTargetStarted"
	EventReasonSHCRolloutAdvanced                   splcommon.EventReason = "SHCRolloutAdvanced"
	EventReasonSHCRolloutCompleted                  splcommon.EventReason = "SHCRolloutCompleted"
	EventReasonSHCScaleDownCancelled                splcommon.EventReason = "SHCScaleDownCancelled"
	EventReasonSHCPodUpdateCancelled                splcommon.EventReason = "SHCPodUpdateCancelled"
	EventReasonSHCAuthorizedRevisionWithdrawn       splcommon.EventReason = "SHCAuthorizedRevisionWithdrawn"
	EventReasonSHCAuthorizedRevisionRecoveryStarted splcommon.EventReason = "SHCAuthorizedRevisionRecoveryStarted"
	EventReasonSHCAuthorizedRevisionRecovered       splcommon.EventReason = "SHCAuthorizedRevisionRecovered"
	EventReasonSHCSearchDrainContinuationApproved   splcommon.EventReason = "SHCSearchDrainContinuationApproved"
	EventReasonSHCInitialFormationRestartStarted    splcommon.EventReason = "SHCInitialFormationRestartStarted"
	EventReasonDependencyNotReady                   splcommon.EventReason = "DependencyNotReady"

	// Warning event reasons — spec & config
	EventReasonValidateSpecFailed      splcommon.EventReason = "ValidateSpecFailed"
	EventReasonApplySplunkConfigFailed splcommon.EventReason = "ApplySplunkConfigFailed"
	EventReasonAppFrameworkInitFailed  splcommon.EventReason = "AppFrameworkInitFailed"
	EventReasonAppRepoConnFailed       splcommon.EventReason = "AppRepositoryConnectionFailed"
	EventReasonSmartStoreConfigPending splcommon.EventReason = "SmartStoreConfigPending"

	// Warning event reasons — services & statefulsets
	EventReasonApplyServiceFailed      splcommon.EventReason = "ApplyServiceFailed"
	EventReasonStatefulSetFailed       splcommon.EventReason = "StatefulSetFailed"
	EventReasonStatefulSetUpdateFailed splcommon.EventReason = "StatefulSetUpdateFailed"
	EventReasonStatefulSetDeleteFailed splcommon.EventReason = "StatefulSetDeleteFailed"
	EventReasonOwnerRefFailed          splcommon.EventReason = "OwnerRefFailed"

	// Warning event reasons — deletion
	EventReasonDeleteFailed splcommon.EventReason = "DeleteFailed"

	// Warning event reasons — secrets & credentials
	EventReasonSecretMissing            splcommon.EventReason = "SecretMissing"
	EventReasonSecretInvalid            splcommon.EventReason = "SecretInvalid"
	EventReasonSecretAccessFailed       splcommon.EventReason = "SecretAccessFailed"
	EventReasonPasswordSyncFailed       splcommon.EventReason = "PasswordSyncFailed"
	EventReasonSHCSecretRotationBlocked splcommon.EventReason = "SHCSecretRotationBlocked"
	EventReasonCertSecretMalformed      splcommon.EventReason = certs.EventReasonCertSecretMalformed

	// Warning event reasons — monitoring console
	EventReasonMonitoringConsoleConfigFailed  splcommon.EventReason = "MonitoringConsoleConfigFailed"
	EventReasonMonitoringConsoleRefFailed     splcommon.EventReason = "MonitoringConsoleRefFailed"
	EventReasonMonitoringConsoleCleanupFailed splcommon.EventReason = "MonitoringConsoleCleanupFailed"
	EventReasonMonitoringConsoleApplyFailed   splcommon.EventReason = "MonitoringConsoleApplyFailed"
	EventReasonAnnotationUpdateFailed         splcommon.EventReason = "AnnotationUpdateFailed"
	EventReasonImageGetFailed                 splcommon.EventReason = "ImageGetFailed"

	// Warning event reasons — cluster operations
	EventReasonResolveQueueObjectStorageFailed splcommon.EventReason = "ResolveQueueObjectStorageFailed"
	EventReasonImmutableRefsModified           splcommon.EventReason = "ImmutableRefsModified"
	EventReasonEmptyClusterManagerRef          splcommon.EventReason = "EmptyClusterManagerRef"
	EventReasonRemoteVolumeKeyCheckFailed      splcommon.EventReason = "RemoteVolumeKeyCheckFailed"
	EventReasonVerifyRFPeersFailed             splcommon.EventReason = "VerifyRFPeersFailed"
	EventReasonMaintenanceModeFailed           splcommon.EventReason = "MaintenanceModeFailed"
	EventReasonRetrieveCMSpecFailed            splcommon.EventReason = "RetrieveCMSpecFailed"
	EventReasonConfFileUpdateFailed            splcommon.EventReason = "ConfFileUpdateFailed"
	EventReasonBundlePushFailed                splcommon.EventReason = "BundlePushFailed"
	EventReasonPodExecFailed                   splcommon.EventReason = "PodExecFailed"
	EventReasonScalingBlockedRF                splcommon.EventReason = "ScalingBlockedRF"
	EventReasonLicenseExpired                  splcommon.EventReason = "LicenseExpired"
	EventReasonLicenseHealthCheckFailed        splcommon.EventReason = "LicenseHealthCheckFailed"

	// Warning event reasons — upgrade
	EventReasonUpgradeCheckFailed            splcommon.EventReason = "UpgradeCheckFailed"
	EventReasonUpgradeBlockedVersionMismatch splcommon.EventReason = "UpgradeBlockedVersionMismatch"
	EventReasonSHCRolloutBlocked             splcommon.EventReason = "SHCRolloutBlocked"

	// Stalled condition transition events
	EventReasonStalled         splcommon.EventReason = "Stalled"
	EventReasonStalledResolved splcommon.EventReason = "StalledResolved"
)
