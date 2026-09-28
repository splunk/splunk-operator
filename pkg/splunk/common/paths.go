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

// List of all Paths used in the Splunk Operator

// List of Splunk Enterprise Paths
const (

	//PeerAppsLoc
	PeerAppsLoc = "etc/peer-apps"

	//ManagerAppsLoc
	ManagerAppsLoc = "etc/manager-apps"

	//SHCluster
	SHCluster = "etc/shcluster"

	//SHClusterAppsLoc = "etc/shcluster/apps"
	SHClusterAppsLoc = SHCluster + "/apps"
)

// List of Operator Paths
const (
	// ConfigToken is the SmartStore ConfigMap token mounted into Splunk pods.
	ConfigToken = "conftoken"

	// SetSymbolicLinkClusterManager restores the SmartStore links after a bundle push.
	SetSymbolicLinkClusterManager = "ln -sfn /mnt/splunk-operator/local/indexes.conf /opt/splunk/etc/manager-apps/splunk-operator/local/indexes.conf && ln -sfn  /mnt/splunk-operator/local/server.conf /opt/splunk/etc/manager-apps/splunk-operator/local/server.conf"

	// CommandForClusterManagerSmartstore initializes SmartStore links in a manager pod.
	CommandForClusterManagerSmartstore = "mkdir -p " + OperatorClusterManagerAppsLocal + " && ln -sfn " + OperatorMountLocalIndexesConf + " " + OperatorClusterManagerAppsLocalIndexesConf + " && ln -sfn " + OperatorMountLocalServerConf + " " + OperatorClusterManagerAppsLocalServerConf

	//ManagerAppsOperatorLocal
	OperatorClusterManagerAppsLocal = "/opt/splk/etc/manager-apps/splunk-operator/local"

	//OperatorClusterManagerAppsLocalIndexesConf
	OperatorClusterManagerAppsLocalIndexesConf = "/opt/splk/etc/manager-apps/splunk-operator/local/indexes.conf"

	//OperatorClusterManagerAppsLocalServerConf
	OperatorClusterManagerAppsLocalServerConf = "/opt/splk/etc/manager-apps/splunk-operator/local/server.conf"

	//OperatorMountLocalIndexesConf
	OperatorMountLocalIndexesConf = "/mnt/splunk-operator/local/indexes.conf"

	//OperatorMountLocalServerConf
	OperatorMountLocalServerConf = "/mnt/splunk-operator/local/server.conf"
)
