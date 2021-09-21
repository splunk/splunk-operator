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

package testenv

import (
	"encoding/json"
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

// ClusterMasterSitesResponse is a representation of the sites managed by a Splunk cluster-manager
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

// ClusterMasterHealthResponse is a representation of the health response by a Splunk cluster-manager
// Endpoint: /services/cluster/master/health
type ClusterMasterHealthResponse struct {
	Entries []ClusterMasterHealthEntry `json:"entry"`
}

// ClusterMasterHealthEntry represents a site of an indexer cluster with its metadata
type ClusterMasterHealthEntry struct {
	Name    string                     `json:"name"`
	Content ClusterMasterHealthContent `json:"content"`
}

// ClusterMasterHealthContent represents detailed information about a site
type ClusterMasterHealthContent struct {
	AllDataIsSearchable      string `json:"all_data_is_searchable"`
	AllPeersAreUp            string `json:"all_peers_are_up"`
	Multisite                string `json:"multisite"`
	NoFixupTasksInProgress   string `json:"no_fixup_tasks_in_progress"`
	ReplicationFactorMet     string `json:"replication_factor_met"`
	SearchFactorMet          string `json:"search_factor_met"`
	SiteReplicationFactorMet string `json:"site_replication_factor_met"`
	SiteSearchFactorMet      string `json:"site_search_factor_met"`
}

// CheckRFSF check if cluster has met replication factor and search factor
func CheckRFSF(deployment *Deployment) bool {
	//code to execute
	podName := fmt.Sprintf("splunk-%s-cluster-master-0", deployment.GetName())
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/master/health?output_mode=json"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	restResponse := ClusterMasterHealthResponse{}
	err = json.Unmarshal([]byte(stdout), &restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse health status")
		return false
	}
	sfMet := restResponse.Entries[0].Content.SearchFactorMet == "1"
	if sfMet == false {
		logf.Log.Info("Search Factor not met")
	}
	rfMet := restResponse.Entries[0].Content.ReplicationFactorMet == "1"
	if rfMet == false {
		logf.Log.Info("Replicaton Factor not met")
	}
	return rfMet && sfMet
}

// ClusterMasterPeersAndSearchHeadResponse /services/cluster/master/peers  and /services/cluster/master/searchhead response
type ClusterMasterPeersAndSearchHeadResponse struct {
	Entry []struct {
		Content struct {
			Label                                  string `json:"label"`
			RegisterSearchAddress                  string `json:"register_search_address"`
			ReplicationCount                       int    `json:"replication_count"`
			ReplicationPort                        int    `json:"replication_port"`
			ReplicationUseSsl                      bool   `json:"replication_use_ssl"`
			RestartRequiredForApplyingDryRunBundle bool   `json:"restart_required_for_applying_dry_run_bundle"`
			Site                                   string `json:"site"`
			SplunkVersion                          string `json:"splunk_version"`
			Status                                 string `json:"status"`
			SummaryReplicationCount                int    `json:"summary_replication_count"`
		} `json:"content"`
	} `json:"entry"`
}

// GetIndexersOrSearchHeadsOnCM get indexers or search head on Cluster Manager
func GetIndexersOrSearchHeadsOnCM(deployment *Deployment, endpoint string) ClusterMasterPeersAndSearchHeadResponse {
	url := ""
	if endpoint == "sh" {
		url = "https://localhost:8089/services/cluster/master/searchheads?output_mode=json"
	} else {
		url = "https://localhost:8089/services/cluster/master/peers?output_mode=json"
	}
	//code to execute
	podName := fmt.Sprintf("splunk-%s-cluster-master-0", deployment.GetName())
	stdin := fmt.Sprintf("curl -ks -u admin:$(cat /mnt/splunk-secrets/password) %s", url)
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	restResponse := ClusterMasterPeersAndSearchHeadResponse{}
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return restResponse
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	err = json.Unmarshal([]byte(stdout), &restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse cluster peers")
	}
	return restResponse
}

// CheckIndexerOnCM check given Indexer on cluster manager
func CheckIndexerOnCM(deployment *Deployment, indexerName string) bool {
	restResponse := GetIndexersOrSearchHeadsOnCM(deployment, "peer")
	found := false
	for _, entry := range restResponse.Entry {
		logf.Log.Info("Peer found On CM", "Indexer Name", entry.Content.Label, "Status", entry.Content.Status)
		if entry.Content.Label == indexerName {
			found = true
			break
		}
	}
	return found
}

// CheckSearchHeadOnCM check given search head on cluster manager
func CheckSearchHeadOnCM(deployment *Deployment, searchHeadName string) bool {
	restResponse := GetIndexersOrSearchHeadsOnCM(deployment, "sh")
	found := false
	for _, entry := range restResponse.Entry {
		logf.Log.Info("Search Head On CM", "Search Head", entry.Content.Label, "Status", entry.Content.Status)
		if entry.Content.Label == searchHeadName {
			found = true
			break
		}
	}
	return found
}

// CheckSearchHeadRemoved check if search head is removed from Indexer Cluster
func CheckSearchHeadRemoved(deployment *Deployment) bool {
	restResponse := GetIndexersOrSearchHeadsOnCM(deployment, "sh")
	searchHeadRemoved := true
	for _, entry := range restResponse.Entry {
		logf.Log.Info("Search Found", "Search Head", entry.Content.Label, "Status", entry.Content.Status)
		if entry.Content.Status == "Disconnected" {
			searchHeadRemoved = false
		}
	}
	return searchHeadRemoved
}

// RollHotBuckets roll hot buckets in cluster
func RollHotBuckets(deployment *Deployment) bool {
	podName := fmt.Sprintf("splunk-%s-cluster-master-0", deployment.GetName())
	stdin := "/opt/splunk/bin/splunk rolling-restart cluster-peers -auth admin:$(cat /mnt/splunk-secrets/password)"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	if strings.Contains(stdout, "Rolling restart of all cluster peers has been initiated.") {
		return true
	}
	return false
}

// RollingRestartEndpointResponse is represtentation of /services/cluster/master/info endpiont
type RollingRestartEndpointResponse struct {
	Entry []struct {
		Content struct {
			RollingRestartFlag bool `json:"rolling_restart_flag"`
		} `json:"content"`
	} `json:"entry"`
}

// CheckRollingRestartStatus checks if rolling restart is happening in cluster
func CheckRollingRestartStatus(deployment *Deployment) bool {
	podName := fmt.Sprintf("splunk-%s-cluster-master-0", deployment.GetName())
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/master/info?output_mode=json"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	restResponse := RollingRestartEndpointResponse{}
	err = json.Unmarshal([]byte(stdout), &restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse cluster searchheads")
		return false
	}
	rollingRestart := true
	for _, entry := range restResponse.Entry {
		rollingRestart = entry.Content.RollingRestartFlag
	}
	return rollingRestart
}
