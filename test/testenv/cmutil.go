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

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"

	"strings"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ClusterMasterSitesResponse is a representation of the sites managed by a Splunk cluster-manager
// Endpoint: /services/cluster/manager/sites
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
// Endpoint: /services/cluster/manager/health
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
	podName := fmt.Sprintf("splunk-%s-%s-0", deployment.GetName(), splcommon.ClusterManager)
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) " + splcommon.LocalURLClusterManagerGetHealth
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

// ClusterMasterPeersAndSearchHeadResponse /services/cluster/manager/peers  and /services/cluster/manager/searchhead response
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
			BundleID                               string `json:"active_bundle_id"`
		} `json:"content"`
	} `json:"entry"`
}

// GetIndexersOrSearchHeadsOnCM get indexers or search head on Cluster Manager
func GetIndexersOrSearchHeadsOnCM(deployment *Deployment, endpoint string) ClusterMasterPeersAndSearchHeadResponse {
	url := ""
	if endpoint == "sh" {
		url = splcommon.LocalURLClusterManagerGetSearchHeads
	} else {
		url = splcommon.LocalURLClusterManagerGetPeersJSONOutput
	}
	//code to execute
	podName := fmt.Sprintf("splunk-%s-%s-0", deployment.GetName(), splcommon.ClusterManager)
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
	podName := fmt.Sprintf("splunk-%s-%s-0", deployment.GetName(), splcommon.ClusterManager)
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

// ClusterMasterInfoEndpointResponse is represtentation of /services/cluster/manager/info endpoint
type ClusterMasterInfoEndpointResponse struct {
	Entry []struct {
		Content struct {
			RollingRestartFlag bool              `json:"rolling_restart_flag"`
			ActiveBundle       map[string]string `json:"active_bundle"`
		} `json:"content"`
	} `json:"entry"`
}

// ClusterMasterInfoResponse Get cluster Manager response
func ClusterMasterInfoResponse(deployment *Deployment, podName string) ClusterMasterInfoEndpointResponse {
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/master/info?output_mode=json"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	restResponse := ClusterMasterInfoEndpointResponse{}
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return restResponse
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	err = json.Unmarshal([]byte(stdout), &restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse cluster Manager response")
		return restResponse
	}
	return restResponse
}

// CheckRollingRestartStatus checks if rolling restart is happening in cluster
func CheckRollingRestartStatus(deployment *Deployment) bool {
	podName := fmt.Sprintf("splunk-%s-%s-0", deployment.GetName(), splcommon.ClusterManager)
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) " + splcommon.LocalURLClusterManagerGetInfoJSONOutput
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	restResponse := ClusterMasterInfoEndpointResponse{}
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

// ClusterManagerBundlePushstatus Check for bundle push status on ClusterManager
func ClusterManagerBundlePushstatus(deployment *Deployment, previousBundleHash string) map[string]string {
	restResponse := GetIndexersOrSearchHeadsOnCM(deployment, "")

	bundleStatus := make(map[string]string)
	for _, entry := range restResponse.Entry {
		// Check if new bundle was pushed by comparing hash
		if previousBundleHash != "" {
			if entry.Content.BundleID == previousBundleHash {
				logf.Log.Info("Bundle hash not updated", "old Bundle hash", previousBundleHash, "new Bundle hash", entry.Content.BundleID)
				continue
			}
		}
		bundleStatus[entry.Content.Label] = entry.Content.Status
	}

	logf.Log.Info("Bundle status", "status", bundleStatus)
	return bundleStatus
}

// GetClusterManagerBundleHash Get the Active bundle hash on ClusterManager
func GetClusterManagerBundleHash(deployment *Deployment) string {
	podName := fmt.Sprintf(ClusterMasterPod, deployment.GetName())
	restResponse := ClusterMasterInfoResponse(deployment, podName)

	bundleHash := restResponse.Entry[0].Content.ActiveBundle["checksum"]
	logf.Log.Info("Bundle Hash on Cluster Manager Found", "Hash", bundleHash)
	return bundleHash
}
