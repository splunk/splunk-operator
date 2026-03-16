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

const (
	// Normal event reasons
	EventReasonScaledUp              = "ScaledUp"
	EventReasonScaledDown            = "ScaledDown"
	EventReasonClusterInitialized    = "ClusterInitialized"
	EventReasonClusterQuorumLost     = "ClusterQuorumLost"
	EventReasonClusterQuorumRestored = "ClusterQuorumRestored"
	EventReasonPasswordSyncCompleted = "PasswordSyncCompleted"

	// Warning event reasons — spec & config
	EventReasonValidateSpecFailed      = "ValidateSpecFailed"
	EventReasonApplySplunkConfigFailed = "ApplySplunkConfigFailed"
	EventReasonAppFrameworkInitFailed  = "AppFrameworkInitFailed"
	EventReasonAppRepoConnFailed       = "AppRepositoryConnectionFailed"
	EventReasonSmartStoreConfigPending = "SmartStoreConfigPending"

	// Warning event reasons — services & statefulsets
	EventReasonApplyServiceFailed      = "ApplyServiceFailed"
	EventReasonStatefulSetFailed       = "StatefulSetFailed"
	EventReasonStatefulSetUpdateFailed = "StatefulSetUpdateFailed"
	EventReasonStatefulSetDeleteFailed = "StatefulSetDeleteFailed"
	EventReasonOwnerRefFailed          = "OwnerRefFailed"

	// Warning event reasons — deletion
	EventReasonDeleteFailed = "DeleteFailed"

	// Warning event reasons — secrets & credentials
	EventReasonSecretMissing      = "SecretMissing"
	EventReasonSecretInvalid      = "SecretInvalid"
	EventReasonSecretAccessFailed = "SecretAccessFailed"
	EventReasonPasswordSyncFailed = "PasswordSyncFailed"

	// Warning event reasons — monitoring console
	EventReasonMonitoringConsoleConfigFailed  = "MonitoringConsoleConfigFailed"
	EventReasonMonitoringConsoleRefFailed     = "MonitoringConsoleRefFailed"
	EventReasonMonitoringConsoleCleanupFailed = "MonitoringConsoleCleanupFailed"
	EventReasonMonitoringConsoleApplyFailed   = "MonitoringConsoleApplyFailed"
	EventReasonAnnotationUpdateFailed         = "AnnotationUpdateFailed"
	EventReasonImageGetFailed                 = "ImageGetFailed"

	// Warning event reasons — cluster operations
	EventReasonRemoteVolumeKeyCheckFailed = "RemoteVolumeKeyCheckFailed"
	EventReasonVerifyRFPeersFailed        = "VerifyRFPeersFailed"
	EventReasonMaintenanceModeFailed      = "MaintenanceModeFailed"
	EventReasonRetrieveCMSpecFailed       = "RetrieveCMSpecFailed"
	EventReasonConfFileUpdateFailed       = "ConfFileUpdateFailed"
	EventReasonBundlePushFailed           = "BundlePushFailed"
	EventReasonPodExecFailed              = "PodExecFailed"
	EventReasonScalingBlockedRF           = "ScalingBlockedRF"
	EventReasonLicenseExpired             = "LicenseExpired"

	// Warning event reasons — upgrade
	EventReasonUpgradeCheckFailed            = "UpgradeCheckFailed"
	EventReasonUpgradeBlockedVersionMismatch = "UpgradeBlockedVersionMismatch"
)
