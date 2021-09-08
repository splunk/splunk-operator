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

package common

const (

	//URLS from sources outside the Operator core
	//Formated to match golang style and lint

	//LocalAPI  =
	LocalAPI = "https://localhost:8089/services"

	//ManagerAPI =
	ManagerAPI = LocalAPI + "/cluster/master"

	//PeerAPI    =
	PeerAPI = LocalAPI + "cluster/slave"

	//ManagerInfoJSON       =
	ManagerInfoJSON = ManagerAPI + "/info?output_mode=json"

	//ManagerInfoCountJSON  =
	ManagerInfoCountJSON = ManagerAPI + "/info?count=0&output_mode=json"

	//ManagerPeersJSON      =
	ManagerPeersJSON = ManagerAPI + "/peers?output_mode=json"

	//ManagerPeersCountJSON =
	ManagerPeersCountJSON = ManagerAPI + "/peers?count=0&output_mode=json"

	//ManagerPeersDecommission =
	ManagerPeersDecommission = PeerAPI + "/control/control/decommission?enforce_counts=1"

	//ManagerPeersInfo         =
	ManagerPeersInfo = PeerAPI + "/info?count=0&output_mode=json"

	//ManagerRemovePeers =
	ManagerRemovePeers = ManagerAPI + "/control/control/remove_peers?peers=D39B1729-E2C5-4273-B9B2-534DA7C2F866"

	//ManagerSitesJSON   =
	ManagerSitesJSON = ManagerAPI + "/sites?output_mode=json"

	//ManagerHealthJSON  =
	ManagerHealthJSON = ManagerAPI + "/health?output_mode=json"

	//ManagerSearchHeadsJSON =
	ManagerSearchHeadsJSON = ManagerAPI + "/searchheads?output_mode=json"

	//LicenserNodeJSON =
	LicenserNodeJSON = LocalAPI + "/licenser/localslave?output_mode=json"

	//IndexerJSON      =
	IndexerJSON = LocalAPI + "/data/indexes?output_mode=json"

	//ServerInfoJSON   =
	ServerInfoJSON = LocalAPI + "/server/info/server-info?count=0&output_mode=json"

	//SearchDMCLicenseManager =
	SearchDMCLicenseManager = LocalAPI + "/search/distributed/groups/dmc_group_license_master/edit"
	
	//PATHS for locations defined outside of Operator core

	//NodeApps path
	PeerApps = "etc/slave-apps"

	//ManagerApps = "etc/master-apps"
	ManagerApps = "etc/master-apps"

	ClusterPeer = "/cluster/slave"
	ClusterPeerInfo = "/services" + ClusterPeer + "/info"

	ClusterManager = "cluster/master"
	ClusterManagerPeers = "/services/" + ClusterManager + "/peers"





)
