// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"context"
	"sync"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
)

const (
	// max. reconcile requeue schedule time(Seconds)
	maxRecDuration time.Duration = 1<<63 - 1

	// Current App framework version
	currentAfwVersion = enterpriseApi.AfwPhase3

	// Max. of parallel installs for a given Pod
	maxParallelInstallsPerPod = 1

	// Max. number of retries to update the CR Status
	maxRetryCountForCRStatusUpdate = 10
)

// InstanceType is used to represent the type of Splunk instance (search head, indexer, etc).
type InstanceType string

const (
	// SplunkStandalone is a single instance of Splunk Enterprise
	SplunkStandalone InstanceType = "standalone"

	// SplunkClusterMaster is the manager node of an indexer cluster, see https://docs.splunk.com/Documentation/Splunk/latest/Indexer/Basicclusterarchitecture
	SplunkClusterMaster InstanceType = splcommon.ClusterManager

	// SplunkClusterManager is the manager node of an indexer cluster, see https://docs.splunk.com/Documentation/Splunk/latest/Indexer/Basicclusterarchitecture
	SplunkClusterManager InstanceType = "cluster-manager"

	// SplunkSearchHead may be a standalone or clustered search head instance
	SplunkSearchHead InstanceType = "search-head"

	// SplunkIndexer may be a standalone or clustered indexer peer
	SplunkIndexer InstanceType = "indexer"

	// SplunkDeployer is an instance that distributes baseline configurations and apps to search head cluster members
	SplunkDeployer InstanceType = "deployer"

	// SplunkLicenseMaster controls one or more license nodes
	SplunkLicenseMaster InstanceType = splcommon.LicenseManager

	// SplunkLicenseManager controls one or more license nodes
	SplunkLicenseManager InstanceType = "license-manager"

	// SplunkMonitoringConsole is a single instance of Splunk monitor for mc
	SplunkMonitoringConsole InstanceType = "monitoring-console"

	// TmpAppDownloadDir is the Operator directory for app framework, when there is no explicit volume specified
	TmpAppDownloadDir string = "/tmp/appframework/"
)

type commonResourceTracker struct {
	// mutex to serialize the access to commonResourceTracker
	mutex sync.Mutex

	// map of resource name:mutex, so that we can serialize get/create/update to common resources such as secrets/configMaps
	mutexMap map[string]*sync.Mutex
}

type globalResourceTracker struct {
	storage *storageTracker

	commonResourceTracker *commonResourceTracker
}

type storageTracker struct {
	// represents the available disk space on operator pod
	availableDiskSpace uint64

	// mutex to serialize the access
	mutex sync.Mutex
}

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
	client splcommon.ControllerClient

	// cr meta object
	cr splcommon.MetaObject

	// statefulset to know replicaset details
	sts *appsv1.StatefulSet

	// isActive indicates if a worker is assigned
	isActive bool

	// waiter reference to inform the caller
	waiter *sync.WaitGroup

	// indicates a fan out worker
	fanOut bool
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

	// Reference to app deploy context
	appDeployContext *enterpriseApi.AppDeploymentContext

	// Scheduler entry time
	afwEntryTime int64

	// additional context used for bundle push logic
	// runtime client
	client splcommon.ControllerClient

	// cr meta object
	cr splcommon.MetaObject

	// statefulset to know replicaset details
	sts *appsv1.StatefulSet
}

// PlaybookImpl is an interface to implement individual playbooks
type PlaybookImpl interface {
	runPlaybook(ctx context.Context) error
}

var _ PlaybookImpl = &localScopePlaybookContext{}

// blank assignment to implement PlaybookImpl
var _ PlaybookImpl = &IdxcPlaybookContext{}

var _ PlaybookImpl = &SHCPlaybookContext{}

// IdxcPlaybookContext is used to implement playbook to push bundle to indexer cluster peers
type IdxcPlaybookContext struct {
	client        splcommon.ControllerClient
	cr            splcommon.MetaObject
	afwPipeline   *AppInstallPipeline
	targetPodName string
	podExecClient splutil.PodExecClientImpl
}

// SHCPlaybookContext is used to implement playbook to push bundle to SHC members
type SHCPlaybookContext struct {
	client               splcommon.ControllerClient
	cr                   splcommon.MetaObject
	afwPipeline          *AppInstallPipeline
	targetPodName        string
	searchHeadCaptainURL string
	podExecClient        splutil.PodExecClientImpl
}

type localScopePlaybookContext struct {
	worker *PipelineWorker

	// semaphore to track only one app install at any time for a given replicaset pod
	sem           chan struct{}
	podExecClient splutil.PodExecClientImpl
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
	case SplunkClusterMaster:
		role = "splunk_cluster_master"
	case SplunkSearchHead:
		role = "splunk_search_head"
	case SplunkIndexer:
		role = "splunk_indexer"
	case SplunkDeployer:
		role = "splunk_deployer"
	case SplunkLicenseMaster:
		role = splcommon.LicenseManagerRole
	case SplunkLicenseManager:
		role = splcommon.LicenseManagerRole
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
	case SplunkClusterMaster:
		kind = "indexer"
	case SplunkIndexer:
		kind = "indexer"
	case SplunkSearchHead:
		kind = "search-head"
	case SplunkDeployer:
		kind = "search-head"
	case SplunkLicenseMaster:
		kind = splcommon.LicenseManager
	case SplunkLicenseManager:
		kind = "license-manager"
	case SplunkMonitoringConsole:
		kind = "monitoring-console"
	}
	return kind
}
