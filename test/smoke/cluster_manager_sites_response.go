// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package smoke

// ClusterManagerSitesResponse is a representation of the sites managed by a Splunk cluster-manager
// Endpoint: /services/cluster/manager/sites
type ClusterManagerSitesResponse struct {
	Entries []ClusterManagerSitesEntry `json:"entry"`
}

// ClusterManagerSitesEntry represents a site of an indexer cluster with its metadata
type ClusterManagerSitesEntry struct {
	Name    string                     `json:"name"`
	Content ClusterManagerSitesContent `json:"content"`
}

// ClusterManagerSitesContent represents detailed information about a site
type ClusterManagerSitesContent struct {
	Peers map[string]ClusterManagerSitesPeer `json:"peers"`
}

// ClusterManagerSitesPeer reprensents an indexer peer member of a site
type ClusterManagerSitesPeer struct {
	ServerName string `json:"server_name"`
}
