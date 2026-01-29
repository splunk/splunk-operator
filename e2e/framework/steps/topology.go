package steps

import (
	"fmt"
	"strings"

	"github.com/splunk/splunk-operator/e2e/framework/splunkd"
	"github.com/splunk/splunk-operator/e2e/framework/topology"
)

// ApplyTopologySession stores topology session data on the execution context.
func ApplyTopologySession(exec *Context, session *topology.Session) map[string]string {
	metadata := map[string]string{
		"namespace":  session.Namespace,
		"base_name":  session.BaseName,
		"topology":   session.Kind,
		"search_pod": session.SearchPod,
	}

	exec.Vars["namespace"] = session.Namespace
	exec.Vars["base_name"] = session.BaseName
	exec.Vars["topology_kind"] = session.Kind
	exec.Vars["topology_ready"] = "true"
	if session.ClusterManagerKind != "" {
		exec.Vars["cluster_manager_kind"] = session.ClusterManagerKind
		metadata["cluster_manager_kind"] = session.ClusterManagerKind
	}
	if (session.Kind == "m4" || session.Kind == "m1") && session.SiteCount > 0 {
		exec.Vars["site_count"] = fmt.Sprintf("%d", session.SiteCount)
		metadata["site_count"] = fmt.Sprintf("%d", session.SiteCount)
	}

	if session.StandaloneName != "" {
		exec.Vars["standalone_name"] = session.StandaloneName
		metadata["standalone_name"] = session.StandaloneName
	}
	if session.ClusterManagerName != "" {
		exec.Vars["cluster_manager_name"] = session.ClusterManagerName
		metadata["cluster_manager_name"] = session.ClusterManagerName
	}
	if len(session.IndexerClusterNames) > 0 {
		exec.Vars["indexer_cluster_names"] = strings.Join(session.IndexerClusterNames, ",")
		metadata["indexer_cluster_names"] = strings.Join(session.IndexerClusterNames, ",")
		if len(session.IndexerClusterNames) == 1 {
			exec.Vars["indexer_cluster_name"] = session.IndexerClusterNames[0]
			metadata["indexer_cluster_name"] = session.IndexerClusterNames[0]
		}
	}
	if session.SearchHeadClusterName != "" {
		exec.Vars["search_head_cluster_name"] = session.SearchHeadClusterName
		metadata["search_head_cluster_name"] = session.SearchHeadClusterName
	}
	if session.SearchPod != "" && exec.Kube != nil {
		exec.Vars["search_pod"] = session.SearchPod
		exec.Splunkd = splunkd.NewClient(exec.Kube, session.Namespace, session.SearchPod)
	}
	return metadata
}
