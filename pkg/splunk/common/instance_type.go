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

import "fmt"

// InstanceType is used to represent the type of Splunk instance (search head, indexer, etc).
type InstanceType string

const (
	// SplunkStandalone is a single instance of Splunk Enterprise
	SplunkStandalone InstanceType = "standalone"

	// SplunkClusterMaster is the manager node of an indexer cluster, see https://docs.splunk.com/Documentation/Splunk/latest/Indexer/Basicclusterarchitecture
	SplunkClusterMaster InstanceType = InstanceType(ClusterManager)

	// SplunkClusterManager is the manager node of an indexer cluster, see https://docs.splunk.com/Documentation/Splunk/latest/Indexer/Basicclusterarchitecture
	SplunkClusterManager InstanceType = "cluster-manager"

	// SplunkSearchHead may be a standalone or clustered search head instance
	SplunkSearchHead InstanceType = "search-head"

	// SplunkIndexer may be a standalone or clustered indexer peer
	SplunkIndexer InstanceType = "indexer"

	// SplunkIngestor may be a standalone or clustered ingestion peer
	SplunkIngestor InstanceType = "ingestor"

	// SplunkQueue is the queue instance
	SplunkQueue InstanceType = "queue"

	// SplunkObjectStorage is the object storage instance
	SplunkObjectStorage InstanceType = "object-storage"

	// SplunkDeployer is an instance that distributes baseline configurations and apps to search head cluster members
	SplunkDeployer InstanceType = "deployer"

	// SplunkLicenseMaster controls one or more license nodes
	SplunkLicenseMaster InstanceType = InstanceType(LicenseManager)

	// SplunkLicenseManager controls one or more license nodes
	SplunkLicenseManager InstanceType = "license-manager"

	// SplunkMonitoringConsole is a single instance of Splunk monitor for mc
	SplunkMonitoringConsole InstanceType = "monitoring-console"
)

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
		role = LicenseManagerRole
	case SplunkLicenseManager:
		role = LicenseManagerRole
	case SplunkMonitoringConsole:
		role = "splunk_monitor"
	case SplunkIngestor:
		role = "splunk_ingestor"
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
		kind = LicenseManager
	case SplunkLicenseManager:
		kind = "license-manager"
	case SplunkMonitoringConsole:
		kind = "monitoring-console"
	case SplunkIngestor:
		kind = "ingestor"
	}
	return kind
}

// KindToInstanceString returns the InstanceType string for a given CRD Kind
func KindToInstanceString(kind string) string {
	switch kind {
	case "ClusterManager":
		return SplunkClusterManager.ToString()
	case "ClusterMaster":
		return SplunkClusterMaster.ToString()
	case "IndexerCluster":
		return SplunkIndexer.ToString()
	case "IngestorCluster":
		return SplunkIngestor.ToString()
	case "Queue":
		return SplunkQueue.ToString()
	case "ObjectStorage":
		return SplunkObjectStorage.ToString()
	case "LicenseManager":
		return SplunkLicenseManager.ToString()
	case "LicenseMaster":
		return SplunkLicenseMaster.ToString()
	case "MonitoringConsole":
		return SplunkMonitoringConsole.ToString()
	case "SearchHeadCluster":
		return SplunkSearchHead.ToString()
	case "SearchHead":
		return SplunkSearchHead.ToString()
	case "Standalone":
		return SplunkStandalone.ToString()
	}
	return ""
}

// GetSplunkServiceName uses a template to name a Kubernetes Service for Splunk instances.
func GetSplunkServiceName(instanceType InstanceType, identifier string, isHeadless bool) string {
	var result string

	if isHeadless {
		result = fmt.Sprintf("splunk-%s-%s-headless", identifier, instanceType)
	} else {
		result = fmt.Sprintf("splunk-%s-%s-service", identifier, instanceType)
	}

	return result
}
