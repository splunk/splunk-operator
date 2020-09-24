package testenv

import (
	"os/exec"
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
func CheckRFSF(ns string, deploymentName string) bool {
	//code to execute
	podName := fmt.Sprintf("splunk-%s-cluster-master-0", deploymentName)
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/master/health?output_mode=json"
	command := fmt.Sprintf("kubectl exec -n %s %s %s", ns, podName, stdin)
	stdout, err := exec.Command("kubectl", "exec", "-n", ns, "--", stdin).Output()
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout)
	siteIndexerResponse := ClusterMasterHealthResponse{}
	err = json.Unmarshal([]byte(stdout), &siteIndexerResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse health status")
		return false
	}
	sfMet := siteIndexerResponse.Entries[0].Content.SearchFactorMet == "1"
	if sfMet == false {
		logf.Log.Info("Search Factor not met")
	}
	rfMet := siteIndexerResponse.Entries[0].Content.ReplicationFactorMet == "1"
	if rfMet == false {
		logf.Log.Info("Replicaton Factor not met")
	}
	return rfMet && sfMet
}
