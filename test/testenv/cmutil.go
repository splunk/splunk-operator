package testenv

import (
	"encoding/json"
	"fmt"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ClusterMasterHealthResponse is a representation of the health response by a Splunk cluster-master
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
