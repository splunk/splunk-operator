/*
Copyright 2021.

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

// Package v4 contains API Schema definitions for the enterprise v4 API group
// +kubebuilder:object:generate=true
// +groupName=enterprise.splunk.com
package v4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

// ErrorType indicates the class of problem that has caused the Host resource
// to enter an error state.
type ErrorType string

const (
	// LicenseManagerPrepareUpgradeError is an error condition occurring when the controller
	// fails to complete all the necessary activities before upgrade.
	LicenseManagerPrepareUpgradeError ErrorType = "license manager prepare upgrade error"
	// LicenseManagerBackupError is an error condition occurring when an attempt to
	// do backup of persistence volume fails.
	LicenseManagerBackupError ErrorType = "license manager backup error"
	// LicenseManagerRestoreError is an error condition occurring when
	// restoring persistence volumes fails.
	LicenseManagerRestoreError ErrorType = "license manager restore error"
	// LicenseManagerUpgradeError is an error condition occurring when the controller
	// is unable upgrade splunk instance.
	LicenseManagerUpgradeError ErrorType = "license manager upgrade error"
	// LicenseManagerVerificationError is an error condition occurring when the
	// verification fails after upgrade is complete.
	LicenseManagerVerificationError ErrorType = "license manager verification error"

	// ClusterManagerPrepareUpgradeError is an error condition occurring when the controller
	// fails to complete all the necessary activities before upgrade.
	ClusterManagerPrepareUpgradeError ErrorType = "cluster manager prepare upgrade error"
	// ClusterManageBackupError is an error condition occurring when an attempt to
	// do backup of persistence volume fails.
	ClusterManageBackupError ErrorType = "cluster manager backup error"
	// ClusterManageRestoreError is an error condition occurring when
	// restoring persistence volumes fails.
	ClusterManageRestoreError ErrorType = "cluster manager restore error"
	// ClusterManageUpgradeError is an error condition occurring when the controller
	// is unable upgrade splunk instance.
	ClusterManageUpgradeError ErrorType = "cluster manager upgrade error"
	// ClusterManageVerificationError is an error condition occurring when the
	// verification fails after upgrade is complete.
	ClusterManageVerificationError ErrorType = "cluster manager verification error"

	// MonitoringConsolePrepareUpgradeError is an error condition occurring when the controller
	// fails to complete all the necessary activities before upgrade.
	MonitoringConsolePrepareUpgradeError ErrorType = "monitoring console prepare upgrade error"
	// MonitoringConsoleBackupError is an error condition occurring when an attempt to
	// do backup of persistence volume fails.
	MonitoringConsoleBackupError ErrorType = "monitoring console backup error"
	// MonitoringConsoleRestoreError is an error condition occurring when
	// restoring persistence volumes fails.
	MonitoringConsoleRestoreError ErrorType = "monitoring console restore error"
	// MonitoringConsoleUpgradeError is an error condition occurring when the controller
	// is unable upgrade splunk instance.
	MonitoringConsoleUpgradeError ErrorType = "monitoring console upgrade error"
	// MonitoringConsoleVerificationError is an error condition occurring when the
	// verification fails after upgrade is complete.
	MonitoringConsoleVerificationError ErrorType = "monitoring console verification error"

	// SearchHeadClusterPrepareUpgradeError is an error condition occurring when the controller
	// fails to complete all the necessary activities before upgrade.
	SearchHeadClusterPrepareUpgradeError ErrorType = "search head cluster prepare upgrade error"
	// SearchHeadClusterBackupError is an error condition occurring when an attempt to
	// do backup of persistence volume fails.
	SearchHeadClusterBackupError ErrorType = "search head cluster backup error"
	// SearchHeadClusterRestoreError is an error condition occurring when
	// restoring persistence volumes fails.
	SearchHeadClusterRestoreError ErrorType = "search head cluster restore error"
	// SearchHeadClusterUpgradeError is an error condition occurring when the controller
	// is unable upgrade splunk instance.
	SearchHeadClusterUpgradeError ErrorType = "search head cluster upgrade error"
	// SearchHeadClusterVerificationError is an error condition occurring when the
	// verification fails after upgrade is complete.
	SearchHeadClusterVerificationError ErrorType = "search head cluster verification error"

	// IndexerClusterPrepareUpgradeError is an error condition occurring when the controller
	// fails to complete all the necessary activities before upgrade.
	IndexerClusterPrepareUpgradeError ErrorType = "indexer cluster prepare upgrade error"
	// IndexerClusterBackupError is an error condition occurring when an attempt to
	// do backup of persistence volume fails.
	IndexerClusterBackupError ErrorType = "indexer cluster backup error"
	// IndexerClusterRestoreError is an error condition occurring when
	// restoring persistence volumes fails.
	IndexerClusterRestoreError ErrorType = "indexer cluster restore error"
	// IndexerClusterUpgradeError is an error condition occurring when the controller
	// is unable upgrade splunk instance.
	IndexerClusterUpgradeError ErrorType = "indexer cluster upgrade error"
	// IndexerClusterVerificationError is an error condition occurring when the
	// verification fails after upgrade is complete.
	IndexerClusterVerificationError ErrorType = "indexer cluster verification error"

	// StandalonePrepareUpgradeError is an error condition occurring when the controller
	// fails to complete all the necessary activities before upgrade.
	StandalonePrepareUpgradeError ErrorType = "standalone prepare upgrade error"
	// StandaloneBackupError is an error condition occurring when an attempt to
	// do backup of persistence volume fails.
	StandaloneBackupError ErrorType = "standalone backup error"
	// StandaloneRestoreError is an error condition occurring when
	// restoring persistence volumes fails.
	StandaloneRestoreError ErrorType = "standalone restore error"
	// StandaloneUpgradeError is an error condition occurring when the controller
	// is unable upgrade splunk instance.
	StandaloneUpgradeError ErrorType = "standalone upgrade error"
	// StandaloneVerificationError is an error condition occurring when the
	// verification fails after upgrade is complete.
	StandaloneVerificationError ErrorType = "standalone verification error"
)

// UpgradeState defines the states the custom resource will report
// about upgrade .
type ProvisioningState string

const (
	// StateNone means the state is unknown
	StateNone ProvisioningState = ""

	// StateStandalonePrepare preparing for upgrade
	// in progress
	StateStandalonePrepare ProvisioningState = "standalone preparing for upgrade"

	// StateStandaloneBackup means backup of pvc
	// before upgrade in progress
	StateStandaloneBackup ProvisioningState = "standalone backup in progress"

	// StateStandaloneRestore means restoring
	// backed up storage in progress
	StateStandaloneRestore ProvisioningState = "standalone restore in progress"

	// StateStandaloneUpgrade means upgrade of
	// standalone splunk instance in progress
	StateStandaloneUpgrade ProvisioningState = "standalone upgrade in progress"

	// StateStandaloneVerification mean verification of
	// standalone after upgrade in progress
	StateStandaloneVerification ProvisioningState = " verification in progress"

	// StateStandaloneReady means lstandalone is ready
	StateStandaloneReady ProvisioningState = " ready"

	// StateStandaloneError means standalone is in error state
	StateStandaloneError ProvisioningState = "standalone in error state"

	// StateLicenseManagerPrepare preparing for upgrade
	// in progress
	StateLicenseManagerPrepare ProvisioningState = "license manager preparing for upgrade"

	// StateLicenseManagerBackup means backup of pvc
	// before upgrade in progress
	StateLicenseManagerBackup ProvisioningState = "license manager backup in progress"

	// StateLicenseManagerRestore means restoring
	// backed up storage in progress
	StateLicenseManagerRestore ProvisioningState = "license manager restore in progress"

	// StateLicenseManagerUpgrade means upgrade of
	// license manager splunk instance in progress
	StateLicenseManagerUpgrade ProvisioningState = "license manager upgrade in progress"

	// StateLicenseManagerVerification mean verification of
	// license manager after upgrade in progress
	StateLicenseManagerVerification ProvisioningState = "license manager verification in progress"

	// StateLicenseManagerReady means license manager is ready
	StateLicenseManagerReady ProvisioningState = "license manager ready"

	// StateLicenseManagerError means license manager is in error state
	StateLicenseManagerError ProvisioningState = "license manager in error state"

	// StateClusterManagerPrepare preparing for upgrade
	// in progress
	StateClusterManagerPrepare ProvisioningState = "cluster manager preparing for upgrade"

	// StateClusterManagerBackup means backup of pvc
	// before upgrade in progress
	StateClusterManagerBackup ProvisioningState = "cluster manager backup in progress"

	// StateClusterManagerRestore means restoring
	// backed up storage in progress
	StateClusterManagerRestore ProvisioningState = "cluster manager restore in progress"

	// StateClusterManagerUpgrade means upgrade of
	// cluster manager splunk instance in progress
	StateClusterManagerUpgrade ProvisioningState = "cluster manager upgrade in progress"

	// StateClusterManagerVerification mean verification of
	// cluster manager after upgrade in progress
	StateClusterManagerVerification ProvisioningState = "cluster manager verification in progress"

	// StateClusterManagerReady means cluster manager is ready
	StateClusterManagerReady ProvisioningState = "cluster manager ready"

	// StateClusterManagerError means cluster manager is in error state
	StateClusterManagerError ProvisioningState = "cluster manager in error state"

	// StateMonitoringConsolePrepare preparing for upgrade
	// in progress
	StateMonitoringConsolePrepare ProvisioningState = "monitoring console preparing for upgrade"

	// StateMonitoringConsoleBackup means backup of pvc
	// before upgrade in progress
	StateMonitoringConsoleBackup ProvisioningState = "monitoring console  backup in progress"

	// StateMonitoringConsoleRestore means restoring
	// backed up storage in progress
	StateMonitoringConsoleRestore ProvisioningState = "monitoring console restore in progress"

	// StateMonitoringConsoleUpgrade means upgrade of
	// monitoring console splunk instance in progress
	StateMonitoringConsoleUpgrade ProvisioningState = "monitoring console upgrade in progress"

	// StateMonitoringConsoleVerification mean verification of
	// monitoring console after upgrade in progress
	StateMonitoringConsoleVerification ProvisioningState = "monitoring console verification in progress"

	// StateMonitoringConsoleReady means monitoring console is ready
	StateMonitoringConsoleReady ProvisioningState = "monitoring console  ready"

	// StateMonitoringConsolerError means monitoring console is in error state
	StateMonitoringConsoleError ProvisioningState = "monitoring console  in error state"

	// StateSearchHeadClusterPrepare preparing for upgrade
	// in progress
	StateSearchHeadClusterPrepare ProvisioningState = "search head cluster preparing for upgrade"

	// StateSearchHeadClusterBackup means backup of pvc
	// before upgrade in progress
	StateSearchHeadClusterBackup ProvisioningState = "search head cluster backup in progress"

	// StateSearchHeadClusterRestore means restoring
	// backed up storage in progress
	StateSearchHeadClusterRestore ProvisioningState = "search head cluster restore in progress"

	// StateSearchHeadClusterUpgrade means upgrade of
	// search head cluster splunk instance in progress
	StateSearchHeadClusterUpgrade ProvisioningState = "search head cluster upgrade in progress"

	// StateSearchHeadClusterVerification mean verification of
	// search head cluster after upgrade in progress
	StateSearchHeadClusterVerification ProvisioningState = "search head cluster verification in progress"

	// StateSearchHeadClusterReady means search head cluster is ready
	StateSearchHeadClusterReady ProvisioningState = "search head cluster ready"

	// StateSearchHeadClusterError means search head cluster is in error state
	StateSearchHeadClusterError ProvisioningState = "search head cluster in error state"

	// StateClusterManagerPrepare preparing for upgrade
	// in progress
	StateIndexerClusterPrepare ProvisioningState = "indexer cluster preparing for upgrade"

	// StateIndexerClusterBackup means backup of pvc
	// before upgrade in progress
	StateIndexerClusterBackup ProvisioningState = "indexer cluster backup in progress"

	// StateIndexerClusterRestore means restoring
	// backed up storage in progress
	StateIndexerClusterRestore ProvisioningState = "indexer cluster restore in progress"

	// StateIndexerClusterUpgrade means upgrade of
	// indexer cluster splunk instance in progress
	StateIndexerClusterUpgrade ProvisioningState = "indexer cluster upgrade in progress"

	// StateIndexerClusterVerification mean verification of
	// indexer cluster after upgrade in progress
	StateIndexerClusterVerification ProvisioningState = "indexer cluster verification in progress"

	// StateIndexerClusterReady means indexer cluster is ready
	StateIndexerClusterReady ProvisioningState = "indexer cluster ready"

	// StateIndexerClusterError means indexer cluster is in error statProvisioningStatee
	StateIndexerClusterError ProvisioningState = "indexer cluster in error state"
)

// ProvisionStatus holds the state information for a single target.
type ProvisionStatus struct {
	// An indiciator for what the provisioner is doing with the host.
	State ProvisioningState `json:"state"`

	// Image holds the details of the last image successfully
	// provisioned.
	Image string `json:"image,omitempty"`
}

// OperationMetric contains metadata about an operation (inspection,
// provisioning, etc.) used for tracking metrics.
type OperationMetric struct {
	// +nullable
	Start metav1.Time `json:"start,omitempty"`
	// +nullable
	End metav1.Time `json:"end,omitempty"`
}

// Duration returns the length of time that was spent on the
// operation. If the operation is not finished, it returns 0.
func (om OperationMetric) Duration() time.Duration {
	if om.Start.IsZero() {
		return 0
	}
	return om.End.Time.Sub(om.Start.Time)
}

// OperationHistory holds information about operations performed on a
// host.
type OperationHistory struct {
	Prepare      OperationMetric `json:"prepare,omitempty"`
	Backup       OperationMetric `json:"backup,omitempty"`
	Restore      OperationMetric `json:"restore,omitempty"`
	Upgrade      OperationMetric `json:"upgrade,omitempty"`
	Verification OperationMetric `json:"verification,omitempty"`
	Ready        OperationMetric `json:"ready,omitempty"`
	Error        OperationMetric `json:"error,omitempty"`
}

// OperationalStatus represents the state of the host
type OperationalStatus string

const (
	// OperationalStatusOK is the status value for when the host is
	// configured correctly and is manageable.
	OperationalStatusOK OperationalStatus = "OK"

	// OperationalStatusPrepareForUpgrade is the status value for when
	// all the necessary things needed for upgrade process to start is
	// complete.
	OperationalStatusPrepareForUpgrade OperationalStatus = "prepared for upgrade"

	// OperationalStatusBackupComplete is the status value for when
	// backup of persistent volume is complete  .
	OperationalStatusBackupComplete OperationalStatus = "backup complete"

	// OperationalStatusRestoreComplete is the status value for when
	// restore of backed up pvc is complete
	OperationalStatusRestoreComplete OperationalStatus = "restore complete"

	// OperationalStatusUpgradeComplete is the status value for when
	// upgrade of splunk image is complete
	OperationalStatusUpgradeComplete OperationalStatus = "upgrade complete"

	// OperationalStatusVerificationComplete is the status value for when
	// verification of splunk after upgrade is complete
	OperationalStatusVerificationComplete OperationalStatus = "verification complete"

	// OperationalStatusError is the status value for when there are
	// any sort of error.
	OperationalStatusError OperationalStatus = "error"
)

// OperationalStatusAllowed represents the allowed values of OperationalStatus
var OperationalStatusAllowed = []string{"", string(OperationalStatusOK),
	string(OperationalStatusPrepareForUpgrade),
	string(OperationalStatusBackupComplete),
	string(OperationalStatusRestoreComplete),
	string(OperationalStatusUpgradeComplete),
	string(OperationalStatusVerificationComplete),
	string(OperationalStatusError)}
