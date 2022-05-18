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

package testenv

import (
	"context"
	"encoding/json"
	"fmt"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"

	"strings"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

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

// ClusterManagerHealthResponse is a representation of the health response by a Splunk cluster-manager
// Endpoint: /services/cluster/manager/health
type ClusterManagerHealthResponse struct {
	Entries []ClusterManagerHealthEntry `json:"entry"`
}

// ClusterManagerHealthEntry represents a site of an indexer cluster with its metadata
type ClusterManagerHealthEntry struct {
	Name    string                      `json:"name"`
	Content ClusterManagerHealthContent `json:"content"`
}

// ClusterManagerHealthContent represents detailed information about a site
type ClusterManagerHealthContent struct {
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
func CheckRFSF(ctx context.Context, deployment *Deployment) bool {
	//code to execute
	podName := fmt.Sprintf("splunk-%s-%s-0", deployment.GetName(), "cluster-manager")
	if strings.Contains(deployment.GetName(), "master") {
		podName = fmt.Sprintf("splunk-%s-%s-0", deployment.GetName(), "cluster-master")
	}
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/manager/health?output_mode=json"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	restResponse := ClusterManagerHealthResponse{}
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

// ClusterManagerPeersAndSearchHeadResponse /services/cluster/manager/peers  and /services/cluster/manager/searchhead response
type ClusterManagerPeersAndSearchHeadResponse struct {
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
func GetIndexersOrSearchHeadsOnCM(ctx context.Context, deployment *Deployment, endpoint string) ClusterManagerPeersAndSearchHeadResponse {
	url := ""
	if endpoint == "sh" {
		url = splcommon.LocalURLClusterManagerGetSearchHeads
	} else {
		url = "https://localhost:8089/services/cluster/manager/peers?output_mode=json"
	}
	//code to execute
	podName := fmt.Sprintf("splunk-%s-%s-0", deployment.GetName(), "cluster-manager")
	stdin := fmt.Sprintf("curl -ks -u admin:$(cat /mnt/splunk-secrets/password) %s", url)
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	restResponse := ClusterManagerPeersAndSearchHeadResponse{}
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
func CheckIndexerOnCM(ctx context.Context, deployment *Deployment, indexerName string) bool {
	restResponse := GetIndexersOrSearchHeadsOnCM(ctx, deployment, "peer")
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
func CheckSearchHeadOnCM(ctx context.Context, deployment *Deployment, searchHeadName string) bool {
	restResponse := GetIndexersOrSearchHeadsOnCM(ctx, deployment, "sh")
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
func CheckSearchHeadRemoved(ctx context.Context, deployment *Deployment) bool {
	restResponse := GetIndexersOrSearchHeadsOnCM(ctx, deployment, "sh")
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
func RollHotBuckets(ctx context.Context, deployment *Deployment) bool {
	podName := fmt.Sprintf("splunk-%s-%s-0", deployment.GetName(), "cluster-manager")
	stdin := "/opt/splunk/bin/splunk rolling-restart cluster-peers -auth admin:$(cat /mnt/splunk-secrets/password)"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
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

// ClusterManagerInfoEndpointResponse is represtentation of /services/cluster/manager/info endpoint
type ClusterManagerInfoEndpointResponse struct {
	Entry []struct {
		Content struct {
			RollingRestartFlag bool `json:"rolling_restart_flag"`
			ActiveBundle       struct {
				BundlePath string `json:"bundle_path"`
				Checksum   string `json:"checksum"`
				Timestamp  int    `json:"timestamp"`
			} `json:"active_bundle"`
		} `json:"content"`
	} `json:"entry"`
}

// ClusterManagerInfoResponse Get cluster Manager response
func ClusterManagerInfoResponse(ctx context.Context, deployment *Deployment, podName string) ClusterManagerInfoEndpointResponse {
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/manager/info?output_mode=json"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	restResponse := ClusterManagerInfoEndpointResponse{}
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
func CheckRollingRestartStatus(ctx context.Context, deployment *Deployment) bool {
	podName := fmt.Sprintf("splunk-%s-%s-0", deployment.GetName(), "cluster-manager")
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/manager/info?output_mode=json"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	restResponse := ClusterManagerInfoEndpointResponse{}
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
func ClusterManagerBundlePushstatus(ctx context.Context, deployment *Deployment, previousBundleHash string) map[string]string {
	restResponse := GetIndexersOrSearchHeadsOnCM(ctx, deployment, "")

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
func GetClusterManagerBundleHash(ctx context.Context, deployment *Deployment) string {
	podName := fmt.Sprintf(ClusterManagerPod, deployment.GetName())
	restResponse := ClusterManagerInfoResponse(ctx, deployment, podName)

	bundleHash := restResponse.Entry[0].Content.ActiveBundle.Checksum
	logf.Log.Info("Bundle Hash on Cluster Manager Found", "Hash", bundleHash)
	return bundleHash
}
