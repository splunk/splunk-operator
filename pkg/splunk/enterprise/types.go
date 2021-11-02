// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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
	"sync"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
)

// InstanceType is used to represent the type of Splunk instance (search head, indexer, etc).
type InstanceType string

const (
	// SplunkStandalone is a single instance of Splunk Enterprise
	SplunkStandalone InstanceType = "standalone"

	// SplunkClusterManager is the manager node of an indexer cluster, see https://docs.splunk.com/Documentation/Splunk/latest/Indexer/Basicclusterarchitecture
	SplunkClusterManager InstanceType = splcommon.ClusterManager

	// SplunkSearchHead may be a standalone or clustered search head instance
	SplunkSearchHead InstanceType = "search-head"

	// SplunkIndexer may be a standalone or clustered indexer peer
	SplunkIndexer InstanceType = "indexer"

	// SplunkDeployer is an instance that distributes baseline configurations and apps to search head cluster members
	SplunkDeployer InstanceType = "deployer"

	// SplunkLicenseManager controls one or more license nodes
	SplunkLicenseManager InstanceType = splcommon.LicenseManager

	// SplunkMonitoringConsole is a single instance of Splunk monitor for mc
	SplunkMonitoringConsole InstanceType = "monitoring-console"
)

const (
	// Max retries for each phase of the App install pipeline
	pipelinePhaseMaxRetryCount = 3

	// Max. time the app framework scheduler waits before trying to yield
	maxRunTimeBeforeAttemptingYield = 90
)

// PipelineWorker represents execution context used to run an app pkg worker thread
type PipelineWorker struct {
	//  to the AppSource Spec entry
	appSrcName string

	// Reference to the App Framework Config
	afwConfig *enterpriseApi.AppFrameworkSpec

	// Reference to the App context from the CR status
	appDeployInfo *enterpriseApi.AppDeploymentInfo

	// Used for pod copy and install
	targetPodName string

	// runtime client
	client *splcommon.ControllerClient

	// cr meta object
	cr splcommon.MetaObject

	// statefulset to know replicaset details
	sts *appsv1.StatefulSet

	// isActive indicates if a worker is assigned
	isActive bool

	// waiter reference to inform the caller
	waiter *sync.WaitGroup
}

// PipelinePhase represents one phase in the overall installation pipeline
type PipelinePhase struct {
	mutex        sync.Mutex
	q            []*PipelineWorker
	msgChannel   chan *PipelineWorker
	workerWaiter sync.WaitGroup
}

// AppInstallPipeline defines the pipeline for the installation activity
type AppInstallPipeline struct {
	// Pipeline Phases: Download, Pod Copy and Install
	pplnPhases map[enterpriseApi.AppPhaseType]*PipelinePhase

	// Used by the scheduler to wait for all the Phases to complete
	phaseWaiter sync.WaitGroup

	// Used by yield logic
	sigTerm chan struct{}
}

// ToString returns a string for a given InstanceType
func (instanceType InstanceType) ToString() string {
	return string(instanceType)
}

// ToRole returns ansible/container role for a given InstanceType
func (instanceType InstanceType) ToRole() string {
	var role string
	switch instanceType {
	case SplunkStandalone:
		role = "splunk_standalone"
	case SplunkClusterManager:
		role = "splunk_cluster_master"
	case SplunkSearchHead:
		role = "splunk_search_head"
	case SplunkIndexer:
		role = "splunk_indexer"
	case SplunkDeployer:
		role = "splunk_deployer"
	case SplunkLicenseManager:
		role = "splunk_license_master"
	case SplunkMonitoringConsole:
		role = "splunk_monitor"
	}
	return role
}

// ToKind returns manager InstanceType for CRD that manages a given InstanceType
func (instanceType InstanceType) ToKind() string {
	var kind string
	switch instanceType {
	case SplunkStandalone:
		kind = "standalone"
	case SplunkClusterManager:
		kind = "indexer"
	case SplunkIndexer:
		kind = "indexer"
	case SplunkSearchHead:
		kind = "search-head"
	case SplunkDeployer:
		kind = "search-head"
	case SplunkLicenseManager:
		kind = splcommon.LicenseManager
	case SplunkMonitoringConsole:
		kind = "monitoring-console"
	}
	return kind
}
