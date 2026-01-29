package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/spec"
	"github.com/splunk/splunk-operator/e2e/framework/splunkd"
)

// RegisterClusterHandlers registers cluster validation steps.
func RegisterClusterHandlers(reg *Registry) {
	reg.Register("assert.cluster.rf_sf", handleAssertClusterRFSF)
	reg.Register("assert.cluster.multisite_sites", handleAssertClusterMultisiteSites)
	reg.Register("cluster.bundle.hash.capture", handleClusterBundleHashCapture)
	reg.Register("assert.cluster.bundle.push", handleAssertClusterBundlePush)
}

func handleAssertClusterRFSF(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	client, err := clusterManagerClient(exec)
	if err != nil {
		return nil, err
	}
	payload, err := client.ManagementRequest(ctx, "GET", "/services/cluster/manager/health", url.Values{"output_mode": []string{"json"}}, nil)
	if err != nil {
		return nil, fmt.Errorf("cluster manager health request failed: %w", err)
	}

	resp := clusterManagerHealthResponse{}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse cluster health response: %w", err)
	}
	if len(resp.Entries) == 0 {
		return nil, fmt.Errorf("cluster health response missing entries")
	}
	health := resp.Entries[0].Content
	if health.ReplicationFactorMet != "1" || health.SearchFactorMet != "1" {
		return nil, fmt.Errorf("rf/sf not met (rf=%s sf=%s)", health.ReplicationFactorMet, health.SearchFactorMet)
	}
	return map[string]string{"rf_sf": "met"}, nil
}

func handleAssertClusterMultisiteSites(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	siteCount := getInt(step.With, "site_count", 0)
	if siteCount == 0 {
		if value, ok := exec.Vars["site_count"]; ok {
			parsed := 0
			if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil {
				siteCount = parsed
			}
		}
	}
	if siteCount == 0 {
		siteCount = 3
	}

	client, err := clusterManagerClient(exec)
	if err != nil {
		return nil, err
	}
	payload, err := client.ManagementRequest(ctx, "GET", "/services/cluster/manager/sites", url.Values{"output_mode": []string{"json"}}, nil)
	if err != nil {
		return nil, fmt.Errorf("cluster manager sites request failed: %w", err)
	}

	resp := clusterManagerSitesResponse{}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse cluster sites response: %w", err)
	}

	baseName := exec.Vars["base_name"]
	if baseName == "" {
		baseName = exec.Vars["cluster_manager_name"]
	}
	expected := expectedSiteIndexerMap(baseName, siteCount)
	actual := mapSitesResponse(resp)

	if !siteMapEqual(expected, actual) {
		return nil, fmt.Errorf("multisite site map mismatch expected=%v actual=%v", expected, actual)
	}
	return map[string]string{"site_count": fmt.Sprintf("%d", siteCount)}, nil
}

func handleClusterBundleHashCapture(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	client, err := clusterManagerClient(exec)
	if err != nil {
		return nil, err
	}
	payload, err := client.ManagementRequest(ctx, "GET", "/services/cluster/manager/info", url.Values{"output_mode": []string{"json"}}, nil)
	if err != nil {
		return nil, fmt.Errorf("cluster manager info request failed: %w", err)
	}
	resp := clusterManagerInfoResponse{}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse cluster manager info response: %w", err)
	}
	if len(resp.Entries) == 0 {
		return nil, fmt.Errorf("cluster manager info response missing entries")
	}
	hash := resp.Entries[0].Content.ActiveBundle.Checksum
	if hash == "" {
		return nil, fmt.Errorf("cluster manager bundle hash missing")
	}
	varKey := strings.TrimSpace(getString(step.With, "var", "last_bundle_hash"))
	if varKey == "" {
		varKey = "last_bundle_hash"
	}
	exec.Vars[varKey] = hash
	return map[string]string{"bundle_hash": hash, "var": varKey}, nil
}

func handleAssertClusterBundlePush(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	client, err := clusterManagerClient(exec)
	if err != nil {
		return nil, err
	}
	expectedStatus := strings.TrimSpace(getString(step.With, "status", "Up"))
	replicas := getInt(step.With, "replicas", 0)
	if replicas < 1 {
		return nil, fmt.Errorf("replicas is required")
	}
	prev := strings.TrimSpace(getString(step.With, "previous_bundle_hash", exec.Vars["last_bundle_hash"]))
	timeout := exec.Config.DefaultTimeout
	if raw := getString(step.With, "timeout", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	interval := 5 * time.Second
	if raw := getString(step.With, "interval", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			interval = parsed
		}
	}

	deadline := time.Now().Add(timeout)
	for {
		payload, err := client.ManagementRequest(ctx, "GET", "/services/cluster/manager/peers", url.Values{"output_mode": []string{"json"}}, nil)
		if err != nil {
			return nil, fmt.Errorf("cluster manager peers request failed: %w", err)
		}
		resp := clusterManagerPeersResponse{}
		if err := json.Unmarshal(payload, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse cluster manager peers response: %w", err)
		}
		count := 0
		for _, entry := range resp.Entries {
			if expectedStatus != "" && entry.Content.Status != expectedStatus {
				continue
			}
			if prev != "" && entry.Content.BundleID == prev {
				continue
			}
			count++
		}
		if count >= replicas {
			return map[string]string{"replicas": fmt.Sprintf("%d", replicas), "matched": fmt.Sprintf("%d", count)}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("cluster bundle push did not reach expected state within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

type clusterManagerHealthResponse struct {
	Entries []clusterManagerHealthEntry `json:"entry"`
}

type clusterManagerHealthEntry struct {
	Content clusterManagerHealthContent `json:"content"`
}

type clusterManagerHealthContent struct {
	ReplicationFactorMet string `json:"replication_factor_met"`
	SearchFactorMet      string `json:"search_factor_met"`
}

type clusterManagerSitesResponse struct {
	Entries []clusterManagerSitesEntry `json:"entry"`
}

type clusterManagerSitesEntry struct {
	Name    string                     `json:"name"`
	Content clusterManagerSitesContent `json:"content"`
}

type clusterManagerSitesContent struct {
	Peers map[string]clusterManagerSitesPeer `json:"peers"`
}

type clusterManagerSitesPeer struct {
	ServerName string `json:"server_name"`
}

type clusterManagerInfoResponse struct {
	Entries []clusterManagerInfoEntry `json:"entry"`
}

type clusterManagerInfoEntry struct {
	Content clusterManagerInfoContent `json:"content"`
}

type clusterManagerInfoContent struct {
	ActiveBundle clusterManagerBundle `json:"active_bundle"`
}

type clusterManagerBundle struct {
	Checksum string `json:"checksum"`
}

type clusterManagerPeersResponse struct {
	Entries []clusterManagerPeersEntry `json:"entry"`
}

type clusterManagerPeersEntry struct {
	Content clusterManagerPeersContent `json:"content"`
}

type clusterManagerPeersContent struct {
	Label    string `json:"label"`
	Status   string `json:"status"`
	BundleID string `json:"active_bundle_id"`
}

func execOnClusterManager(ctx context.Context, exec *Context, cmd string) (string, string, error) {
	return "", "", fmt.Errorf("execOnClusterManager is deprecated")
}

func clusterManagerClient(exec *Context) (*splunkd.Client, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := strings.TrimSpace(exec.Vars["namespace"])
	if namespace == "" {
		return nil, fmt.Errorf("namespace not set")
	}
	cmName := exec.Vars["cluster_manager_name"]
	if cmName == "" {
		cmName = exec.Vars["base_name"]
	}
	if cmName == "" {
		return nil, fmt.Errorf("cluster manager name not set")
	}
	role := "cluster-manager"
	if strings.EqualFold(exec.Vars["cluster_manager_kind"], "master") {
		role = "cluster-master"
	}
	podName := fmt.Sprintf("splunk-%s-%s-0", cmName, role)
	client := splunkd.NewClient(exec.Kube, namespace, podName)
	if secretName := strings.TrimSpace(exec.Vars["secret_name"]); secretName != "" {
		client = client.WithSecretName(secretName)
	}
	return client, nil
}

func expectedSiteIndexerMap(baseName string, siteCount int) map[string][]string {
	siteIndexerMap := make(map[string][]string, siteCount)
	for site := 1; site <= siteCount; site++ {
		siteName := fmt.Sprintf("site%d", site)
		siteIndexerMap[siteName] = []string{fmt.Sprintf("splunk-%s-site%d-indexer-0", baseName, site)}
	}
	return siteIndexerMap
}

func mapSitesResponse(resp clusterManagerSitesResponse) map[string][]string {
	actual := make(map[string][]string, len(resp.Entries))
	for _, site := range resp.Entries {
		peers := make([]string, 0, len(site.Content.Peers))
		for _, peer := range site.Content.Peers {
			if peer.ServerName != "" {
				peers = append(peers, peer.ServerName)
			}
		}
		sort.Strings(peers)
		actual[site.Name] = peers
	}
	return actual
}

func siteMapEqual(expected, actual map[string][]string) bool {
	if len(expected) != len(actual) {
		return false
	}
	for site, expectedPeers := range expected {
		actualPeers, ok := actual[site]
		if !ok {
			return false
		}
		sort.Strings(expectedPeers)
		sort.Strings(actualPeers)
		if !reflect.DeepEqual(expectedPeers, actualPeers) {
			return false
		}
	}
	return true
}
