// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package basic

// ClusterMasterSitesResponse is a representation of the sites managed by a Splunk cluster-master
// Endpoint: /services/cluster/master/sites
type ClusterMasterSitesResponse struct {
	Entries []ClusterMasterSitesEntry `json:"entry"`
}

// ClusterMasterSitesEntry represents a site of an indexer cluster with its metadata
type ClusterMasterSitesEntry struct {
	Name    string                    `json:"name"`
	Content ClusterMasterSitesContent `json:"content"`
}

// ClusterMasterSitesContent represents detailed information about a site
type ClusterMasterSitesContent struct {
	Peers map[string]ClusterMasterSitesPeer `json:"peers"`
}

// ClusterMasterSitesPeer reprensents an indexer peer member of a site
type ClusterMasterSitesPeer struct {
	ServerName string `json:"server_name"`
}
