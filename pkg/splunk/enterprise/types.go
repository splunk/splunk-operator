// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

// InstanceType is used to represent the type of Splunk instance (search head, indexer, etc).
type InstanceType string

const (
	// SplunkStandalone is a single instance of Splunk Enterprise
	SplunkStandalone InstanceType = "standalone"

	// SplunkClusterMaster is the master node of an indexer cluster, see https://docs.splunk.com/Documentation/Splunk/latest/Indexer/Basicclusterarchitecture
	SplunkClusterMaster InstanceType = "cluster-master"

	// SplunkSearchHead may be a standalone or clustered search head instance
	SplunkSearchHead InstanceType = "search-head"

	// SplunkIndexer may be a standalone or clustered indexer peer
	SplunkIndexer InstanceType = "indexer"

	// SplunkDeployer is an instance that distributes baseline configurations and apps to search head cluster members
	SplunkDeployer InstanceType = "deployer"

	// SplunkLicenseMaster controls one or more license slaves
	SplunkLicenseMaster InstanceType = "license-master"
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
	case SplunkClusterMaster:
		role = "splunk_cluster_master"
	case SplunkSearchHead:
		role = "splunk_search_head"
	case SplunkIndexer:
		role = "splunk_indexer"
	case SplunkDeployer:
		role = "splunk_deployer"
	case SplunkLicenseMaster:
		role = "splunk_license_master"
	}
	return role
}

// ToKind returns master InstanceType for CRD that manages a given InstanceType
func (instanceType InstanceType) ToKind() string {
	var kind string
	switch instanceType {
	case SplunkStandalone:
		kind = "standalone"
	case SplunkClusterMaster:
		kind = "indexer"
	case SplunkIndexer:
		kind = "indexer"
	case SplunkSearchHead:
		kind = "search-head"
	case SplunkDeployer:
		kind = "search-head"
	case SplunkLicenseMaster:
		kind = "license-master"
	}
	return kind
}
