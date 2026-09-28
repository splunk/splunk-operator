// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package upgrade

import (
	"fmt"

	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

const phaseReady = "Ready"

// State contains the dependency state needed to decide whether an upgrade may proceed.
type State struct {
	Kind                          string
	Name                          string
	Image                         string
	HasLicenseManagerRef          bool
	HasClusterManagerRef          bool
	ValidateIndexerCluster        bool
	LicenseManager                *ResourceState
	ClusterManager                *ResourceState
	PreviousIndexer               *ResourceState
	SearchHeadCluster             *ResourceState
	MonitoringConsoleDependencies []ResourceState
	ClusterInfo                   *splclient.ClusterInfo
}

// ResourceState contains the upgrade-relevant state for one dependent resource.
type ResourceState struct {
	Type  string
	Name  string
	Image string
	Phase string
}

// Event describes a Kubernetes event the reconcile adapter should emit.
type Event struct {
	Reason  string
	Message string
}

// Result is the workflow decision for the reconcile adapter.
type Result struct {
	Continue bool
	Events   []Event
}

// Validate decides whether reconciliation should continue based on collected upgrade state.
func Validate(state State) (Result, error) {
	if state.Kind == "Standalone" || state.Kind == "LicenseManager" {
		return continueResult(), nil
	}

	if state.HasLicenseManagerRef && state.LicenseManager != nil {
		if state.LicenseManager.Image != state.Image {
			return waitResult(), fmt.Errorf("license manager current image (%s) is different than CR image (%s)", state.LicenseManager.Image, state.Image)
		}
		if state.LicenseManager.Phase != phaseReady {
			return waitResult(), nil
		}
	}

	if state.Kind == "ClusterManager" {
		return continueResult(), nil
	}

	if state.ClusterManager != nil {
		if state.ClusterManager.Phase != phaseReady {
			return waitResult(), fmt.Errorf("cluster manager %s is not ready (phase: %s). IndexerCluster upgrade is waiting for ClusterManager to be ready", state.ClusterManager.Name, state.ClusterManager.Phase)
		}
		if state.ClusterManager.Image != state.Image {
			message := fmt.Sprintf("Upgrade blocked: ClusterManager version %s != IndexerCluster version %s. Upgrade ClusterManager first.", state.ClusterManager.Image, state.Image)
			return Result{
				Continue: false,
				Events: []Event{{
					Reason:  string(splcommon.EventReasonUpgradeBlockedVersionMismatch),
					Message: message,
				}},
			}, fmt.Errorf("cluster manager %s image (%s) does not match IndexerCluster image (%s). Please upgrade ClusterManager and IndexerCluster together using the operator's RELATED_IMAGE_SPLUNK_ENTERPRISE or upgrade the ClusterManager first", state.ClusterManager.Name, state.ClusterManager.Image, state.Image)
		}
	}

	if state.Kind == "IndexerCluster" && state.ValidateIndexerCluster {
		if state.ClusterInfo != nil && state.ClusterInfo.MultiSite == "true" && state.PreviousIndexer != nil {
			if state.PreviousIndexer.Phase != phaseReady || state.PreviousIndexer.Image != state.Image {
				return waitResult(), nil
			}
		}
		return continueResult(), nil
	}

	if state.Kind == "SearchHeadCluster" {
		return continueResult(), nil
	}

	if state.SearchHeadCluster != nil {
		if state.SearchHeadCluster.Phase != phaseReady || state.SearchHeadCluster.Image != state.Image {
			return waitResult(), nil
		}
	}

	if state.Kind == "MonitoringConsole" {
		for _, dependency := range state.MonitoringConsoleDependencies {
			if dependency.Phase != phaseReady {
				return waitResult(), fmt.Errorf("%s %s is not ready", dependency.Type, dependency.Name)
			}
		}
	}

	return continueResult(), nil
}

func continueResult() Result {
	return Result{Continue: true}
}

func waitResult() Result {
	return Result{Continue: false}
}
