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

package client

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	//logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// kubernetes logger used by splunk.enterprise package
//var log = logf.Log.WithName("splunk.client")

// SplunkHTTPClient defines the interface used by SplunkClient.
// It is used to mock alternative implementations used for testing.
type SplunkHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// SplunkClient is a simple object used to send HTTP REST API requests
type SplunkClient struct {
	// https endpoint for management interface (e.g. "https://server:8089")
	ManagementURI string

	// username for authentication
	Username string

	// password for authentication
	Password string

	// HTTP client used to process requests
	Client SplunkHTTPClient
}

// NewSplunkClient returns a new SplunkClient object initialized with a username and password.
func NewSplunkClient(managementURI, username, password string) *SplunkClient {
	return &SplunkClient{
		ManagementURI: managementURI,
		Username:      username,
		Password:      password,
		Client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // don't verify ssl certs
			},
		},
	}
}

// Do processes a Splunk REST API request and unmarshals response into obj, if not nil.
func (c *SplunkClient) Do(request *http.Request, expectedStatus []int, obj interface{}) error {
	// send HTTP response and check status
	request.SetBasicAuth(c.Username, c.Password)
	response, err := c.Client.Do(request)
	if err != nil {
		return err
	}
	//default set flag to false and the check response code
	expectedStatusFlag := false
	for i := 0; i < len(expectedStatus); i++ {
		if expectedStatus[i] == response.StatusCode {
			expectedStatusFlag = true
			break
		}
	}
	if !expectedStatusFlag {
		return fmt.Errorf("response code=%d from %s; want %d", response.StatusCode, request.URL, expectedStatus)
	}
	if obj == nil {
		return nil
	}

	// unmarshall response if obj != nil
	data, _ := ioutil.ReadAll(response.Body)
	if len(data) == 0 {
		return fmt.Errorf("received empty response body from %s", request.URL)
	}
	return json.Unmarshal(data, obj)
}

// Get sends a REST API request and unmarshals response into obj, if not nil.
func (c *SplunkClient) Get(path string, obj interface{}) error {
	endpoint := fmt.Sprintf("%s%s?count=0&output_mode=json", c.ManagementURI, path)
	request, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return err
	}
	expectedStatus := []int{200}
	return c.Do(request, expectedStatus, obj)
}

// SearchHeadCaptainInfo represents the status of the search head cluster.
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#shcluster.2Fcaptain.2Finfo
type SearchHeadCaptainInfo struct {
	// Id of this SH cluster. This is used as the unique identifier for the Search Head Cluster in bundle replication and acceleration summary management.
	Identifier string `json:"id"`

	// Time when the current captain was elected
	ElectedCaptain int64 `json:"elected_captain"`

	// Indicates if the searchhead cluster is initialized.
	Initialized bool `json:"initialized_flag"`

	// The name for the captain. Displayed on the Splunk Web manager page.
	Label string `json:"label"`

	// Indicates if the cluster is in maintenance mode.
	MaintenanceMode bool `json:"maintenance_mode"`

	// Flag to indicate if more then replication_factor peers have joined the cluster.
	MinPeersJoined bool `json:"min_peers_joined_flag"`

	// URI of the current captain.
	PeerSchemeHostPort string `json:"peer_scheme_host_port"`

	// Indicates whether the captain is restarting the members in a searchhead cluster.
	RollingRestart bool `json:"rolling_restart_flag"`

	// Indicates whether the captain is ready to begin servicing, based on whether it is initialized.
	ServiceReady bool `json:"service_ready_flag"`

	// Timestamp corresponding to the creation of the captain.
	StartTime int64 `json:"start_time"`
}

// GetSearchHeadCaptainInfo queries the captain for info about the search head cluster.
// You can use this on any member of a search head cluster.
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#shcluster.2Fcaptain.2Finfo
func (c *SplunkClient) GetSearchHeadCaptainInfo() (*SearchHeadCaptainInfo, error) {
	apiResponse := struct {
		Entry []struct {
			Content SearchHeadCaptainInfo `json:"content"`
		} `json:"entry"`
	}{}
	path := "/services/shcluster/captain/info"
	err := c.Get(path, &apiResponse)
	if err != nil {
		return nil, err
	}
	if len(apiResponse.Entry) < 1 {
		return nil, fmt.Errorf("invalid response from %s%s", c.ManagementURI, path)
	}
	return &apiResponse.Entry[0].Content, nil
}

// SearchHeadCaptainMemberInfo represents the status of a search head cluster member (captain endpoint).
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#shcluster.2Fcaptain.2Fmembers
type SearchHeadCaptainMemberInfo struct {
	// Flag that indicates if this member can run scheduled searches.
	Adhoc bool `json:"adhoc_searchhead"`

	// Flag to indicate if this peer advertised that it needed a restart.
	AdvertiseRestartRequired bool `json:"advertise_restart_required"`

	// Number of artifacts on this peer.
	ArtifactCount int `json:"artifact_count"`

	// The host and management port advertised by this peer.
	HostPortPair string `json:"host_port_pair"`

	// True if this member is the SHC captain.
	Captain bool `json:"is_captain"`

	// Host and port of the kv store instance of this member.
	KVStoreHostPort string `json:"kv_store_host_port"`

	// The name for this member. Displayed on the Splunk Web manager page.
	Label string `json:"label"`

	// Timestamp for last heartbeat received from the peer
	LastHeartbeat int64 `json:"last_heartbeat"`

	// REST API endpoint for management
	ManagementURI string `json:"mgmt_url"`

	// URI of the current captain.
	PeerSchemeHostPort string `json:"peer_scheme_host_port"`

	// Used by the captain to keep track of pending jobs requested by the captain to this member.
	PendingJobCount int `json:"pending_job_count"`

	// Number of replications this peer is part of, as either source or target.
	ReplicationCount int `json:"replication_count"`

	// TCP port to listen for replicated data from another cluster member.
	ReplicationPort int `json:"replication_port"`

	// Indicates whether to use SSL when sending replication data.
	ReplicationUseSSL bool `json:"replication_use_ssl"`

	// Indicates the status of the member.
	Status string `json:"status"`
}

// GetSearchHeadCaptainMembers queries the search head captain for info about cluster members.
// You can only use this on a search head cluster captain.
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#shcluster.2Fcaptain.2Fmembers
func (c *SplunkClient) GetSearchHeadCaptainMembers() (map[string]SearchHeadCaptainMemberInfo, error) {
	apiResponse := struct {
		Entry []struct {
			Content SearchHeadCaptainMemberInfo `json:"content"`
		} `json:"entry"`
	}{}
	path := "/services/shcluster/captain/members"
	err := c.Get(path, &apiResponse)
	if err != nil {
		return nil, err
	}

	members := make(map[string]SearchHeadCaptainMemberInfo)
	for _, e := range apiResponse.Entry {
		members[e.Content.Label] = e.Content
	}

	return members, nil
}

// SearchHeadClusterMemberInfo represents the status of a search head cluster member.
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#shcluster.2Fmember.2Finfo
type SearchHeadClusterMemberInfo struct {
	// Number of currently running historical searches.
	ActiveHistoricalSearchCount int `json:"active_historical_search_count"`

	// Number of currently running realtime searches.
	ActiveRealtimeSearchCount int `json:"active_realtime_search_count"`

	// Flag that indicates if this member can run scheduled searches.
	Adhoc bool `json:"adhoc_searchhead"`

	// Indicates if this member is registered with the searchhead cluster captain.
	Registered bool `json:"is_registered"`

	// Timestamp for the last attempt to contact the captain.
	LastHeartbeatAttempt int64 `json:"last_heartbeat_attempt"`

	// Number of scheduled searches run in the last 15 minutes.
	PeerLoadStatsGla15m int `json:"peer_load_stats_gla_15m"`

	// Number of scheduled searches run in the last one minute.
	PeerLoadStatsGla1m int `json:"peer_load_stats_gla_1m"`

	// Number of scheduled searches run in the last five minutes.
	PeerLoadStatsGla5m int `json:"peer_load_stats_gla_5m"`

	// Indicates whether the member needs to be restarted to enable its searchhead cluster configuration.
	RestartState string `json:"restart_state"`

	// Indicates the status of the member.
	Status string `json:"status"`
}

// GetSearchHeadClusterMemberInfo queries info from a search head cluster member.
// You can use this on any member of a search head cluster.
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#shcluster.2Fmember.2Finfo
func (c *SplunkClient) GetSearchHeadClusterMemberInfo() (*SearchHeadClusterMemberInfo, error) {
	apiResponse := struct {
		Entry []struct {
			Content SearchHeadClusterMemberInfo `json:"content"`
		} `json:"entry"`
	}{}
	path := "/services/shcluster/member/info"
	err := c.Get(path, &apiResponse)
	if err != nil {
		return nil, err
	}
	if len(apiResponse.Entry) < 1 {
		return nil, fmt.Errorf("invalid response from %s%s", c.ManagementURI, path)
	}
	return &apiResponse.Entry[0].Content, nil
}

// SetSearchHeadDetention enables or disables detention of a search head cluster member.
// You can use this on any member of a search head cluster.
// See https://docs.splunk.com/Documentation/Splunk/latest/DistSearch/SHdetention
func (c *SplunkClient) SetSearchHeadDetention(detain bool) error {
	mode := "off"
	if detain {
		mode = "on"
	}
	endpoint := fmt.Sprintf("%s/services/shcluster/member/control/control/set_manual_detention?manual_detention=%s", c.ManagementURI, mode)
	request, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}
	expectedStatus := []int{200}
	return c.Do(request, expectedStatus, nil)
}

// RemoveSearchHeadClusterMember removes a search head cluster member.
// You can use this on any member of a search head cluster.
// See https://docs.splunk.com/Documentation/Splunk/latest/DistSearch/Removeaclustermember
func (c *SplunkClient) RemoveSearchHeadClusterMember() error {
	// sent request to remove from search head cluster consensus
	endpoint := fmt.Sprintf("%s/services/shcluster/member/consensus/default/remove_server?output_mode=json", c.ManagementURI)
	request, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}

	// send HTTP response and check status
	request.SetBasicAuth(c.Username, c.Password)
	response, err := c.Client.Do(request)
	if err != nil {
		return err
	}
	if response.StatusCode == 200 {
		return nil
	}
	if response.StatusCode != 503 {
		return fmt.Errorf("response code=%d from %s; want %d", response.StatusCode, request.URL, 200)
	}

	// unmarshall 503 response
	apiResponse := struct {
		Messages []struct {
			Text string `json:"text"`
		} `json:"messages"`
	}{}
	data, _ := ioutil.ReadAll(response.Body)
	if len(data) == 0 {
		return fmt.Errorf("received 503 response with empty body from %s", request.URL)
	}
	err = json.Unmarshal(data, &apiResponse)
	if err != nil {
		return fmt.Errorf("failed to unmarshal response from %s: %v", request.URL, err)
	}

	// check if request failed because member was already removed
	if len(apiResponse.Messages) == 0 {
		return fmt.Errorf("received 503 response with empty Messages from %s", request.URL)
	}
	msg1 := regexp.MustCompile(`Server .* is not part of configuration, hence cannot be removed`)
	msg2 := regexp.MustCompile(`This node is not part of any cluster configuration`)
	if msg1.Match([]byte(apiResponse.Messages[0].Text)) || msg2.Match([]byte(apiResponse.Messages[0].Text)) {
		// it was already removed -> ignore error
		return nil
	}

	return fmt.Errorf("received unrecognized 503 response from %s", request.URL)
}

// ClusterBundleInfo represents the status of a configuration bundle.
type ClusterBundleInfo struct {
	// BundlePath is filesystem path to the file represending the bundle
	BundlePath string `json:"bundle_path"`

	// Checksum used to verify bundle integrity
	Checksum string `json:"checksum"`

	// Timestamp of the bundle
	Timestamp int64 `json:"timestamp"`
}

// ClusterManagerInfo represents the status of the indexer cluster manager.
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#cluster.2Fmanager.2Finfo
type ClusterManagerInfo struct {
	// Indicates if the cluster is initialized.
	Initialized bool `json:"initialized_flag"`

	// Indicates if the cluster is ready for indexing.
	IndexingReady bool `json:"indexing_ready_flag"`

	// Indicates whether the manager is ready to begin servicing, based on whether it is initialized.
	ServiceReady bool `json:"service_ready_flag"`

	// Indicates if the cluster is in maintenance mode.
	MaintenanceMode bool `json:"maintenance_mode"`

	// Indicates whether the manager is restarting the peers in a cluster.
	RollingRestart bool `json:"rolling_restart_flag"`

	// The name for the manager. Displayed in the Splunk Web manager page.
	Label string `json:"label"`

	// Provides information about the active bundle for this manager.
	ActiveBundle ClusterBundleInfo `json:"active_bundle"`

	// The most recent information reflecting any changes made to the manager-apps configuration bundle.
	// In steady state, this is equal to active_bundle. If it is not equal, then pushing the latest bundle to all peers is in process (or needs to be started).
	LatestBundle ClusterBundleInfo `json:"latest_bundle"`

	// Timestamp corresponding to the creation of the manager.
	StartTime int64 `json:"start_time"`
}

// GetClusterManagerInfo queries the cluster manager for info about the indexer cluster.
// You can only use this on a cluster manager.
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#cluster.2Fmanager.2Finfo
func (c *SplunkClient) GetClusterManagerInfo() (*ClusterManagerInfo, error) {
	apiResponse := struct {
		Entry []struct {
			Content ClusterManagerInfo `json:"content"`
		} `json:"entry"`
	}{}
	path := "/services/cluster/manager/info"
	err := c.Get(path, &apiResponse)
	if err != nil {
		return nil, err
	}
	if len(apiResponse.Entry) < 1 {
		return nil, fmt.Errorf("invalid response from %s%s", c.ManagementURI, path)
	}
	return &apiResponse.Entry[0].Content, nil
}

// IndexerClusterPeerInfo represents the status of a indexer cluster peer.
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#cluster.2Fpeer.2Finfo
type IndexerClusterPeerInfo struct {
	// Current bundle being used by this peer.
	ActiveBundle ClusterBundleInfo `json:"active_bundle"`

	// Lists information about the most recent bundle downloaded from the manager.
	LatestBundle ClusterBundleInfo `json:"latest_bundle"`

	// The initial bundle generation ID recognized by this peer. Any searches from previous generations fail.
	// The initial bundle generation ID is created when a peer first comes online, restarts, or recontacts the manager.
	// Note that this is reported as a very large number (18446744073709552000) that breaks Go's JSON library, while the peer is being decommissioned.
	//BaseGenerationID uint64 `json:"base_generation_id"`

	// Indicates if this peer is registered with the manager in the cluster.
	Registered bool `json:"is_registered"`

	// Timestamp for the last attempt to contact the manager.
	LastHeartbeatAttempt int64 `json:"last_heartbeat_attempt"`

	// Indicates whether the peer needs to be restarted to enable its cluster configuration.
	RestartState string `json:"restart_state"`

	// Indicates the status of the peer.
	Status string `json:"status"`
}

// GetIndexerClusterPeerInfo queries info from a indexer cluster peer.
// You can use this on any peer in an indexer cluster.
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#cluster.2Fpeer.2Finfo
func (c *SplunkClient) GetIndexerClusterPeerInfo() (*IndexerClusterPeerInfo, error) {
	apiResponse := struct {
		Entry []struct {
			Content IndexerClusterPeerInfo `json:"content"`
		} `json:"entry"`
	}{}
	path := "/services/cluster/peer/info"
	err := c.Get(path, &apiResponse)
	if err != nil {
		return nil, err
	}
	if len(apiResponse.Entry) < 1 {
		return nil, fmt.Errorf("invalid response from %s%s", c.ManagementURI, path)
	}
	return &apiResponse.Entry[0].Content, nil
}

// ClusterManagerPeerInfo represents the status of a indexer cluster peer (cluster manager endpoint).
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#cluster.2Fmanager.2Fpeers
type ClusterManagerPeerInfo struct {
	// Unique identifier or GUID for the peer
	ID string `json:"guid"`

	// The name for the peer. Displayed on the manager page.
	Label string `json:"label"`

	// The ID of the configuration bundle currently being used by the manager.
	ActiveBundleID string `json:"active_bundle_id"`

	// The initial bundle generation ID recognized by this peer. Any searches from previous generations fail.
	// The initial bundle generation ID is created when a peer first comes online, restarts, or recontacts the manager.
	// Note that this is reported as a very large number (18446744073709552000) that breaks Go's JSON library, while the peer is being decommissioned.
	//BaseGenerationID uint64 `json:"base_generation_id"`

	// Count of the number of buckets on this peer, across all indexes.
	BucketCount int64 `json:"bucket_count"`

	// Count of the number of buckets by index on this peer.
	BucketCountByIndex map[string]int64 `json:"bucket_count_by_index"`

	// Flag indicating if this peer has started heartbeating.
	HeartbeatStarted bool `json:"heartbeat_started"`

	// The host and port advertised to peers for the data replication channel.
	// Can be either of the form IP:port or hostname:port.
	HostPortPair string `json:"host_port_pair"`

	// Flag indicating if this peer belongs to the current committed generation and is searchable.
	Searchable bool `json:"is_searchable"`

	// Timestamp for last heartbeat received from the peer.
	LastHeartbeat int64 `json:"last_heartbeat"`

	// The ID of the configuration bundle this peer is using.
	LatestBundleID string `json:"latest_bundle_id"`

	// Used by the manager to keep track of pending jobs requested by the manager to this peer.
	PendingJobCount int `json:"pending_job_count"`

	// Number of buckets for which the peer is primary in its local site, or the number of buckets that return search results from same site as the peer.
	PrimaryCount int64 `json:"primary_count"`

	// Number of buckets for which the peer is primary that are not in its local site.
	PrimaryCountRemote int64 `json:"primary_count_remote"`

	// Number of replications this peer is part of, as either source or target.
	ReplicationCount int `json:"replication_count"`

	// TCP port to listen for replicated data from another cluster member.
	ReplicationPort int `json:"replication_port"`

	// Indicates whether to use SSL when sending replication data.
	ReplicationUseSSL bool `json:"replication_use_ssl"`

	// To which site the peer belongs.
	Site string `json:"site"`

	// Indicates the status of the peer.
	Status string `json:"status"`

	// Lists the number of buckets on the peer for each search state for the bucket.
	SearchStateCounter struct {
		Searchable            int64 `json:"Searchable"`
		Unsearchable          int64 `json:"Unsearchable"`
		PendingSearchable     int64 `json:"PendingSearchable"`
		SearchablePendingMask int64 `json:"SearchablePendingMask"`
	} `json:"search_state_counter"`

	// Lists the number of buckets on the peer for each bucket status.
	StatusCounter struct {
		// complete (warm/cold) bucket
		Complete int64 `json:"Complete"`

		//  target of replication for already completed (warm/cold) bucket
		NonStreamingTarget int64 `json:"NonStreamingTarget"`

		// bucket pending truncation
		PendingTruncate int64 `json:"PendingTruncate"`

		// bucket pending discard
		PendingDiscard int64 `json:"PendingDiscard"`

		// bucket that is not replicated
		Standalone int64 `json:"Standalone"`

		// copy of streaming bucket where some error was encountered
		StreamingError int64 `json:"StreamingError"`

		// streaming hot bucket on source side
		StreamingSource int64 `json:"StreamingSource"`

		// streaming hot bucket copy on target side
		StreamingTarget int64 `json:"StreamingTarget"`

		// uninitialized
		Unset int64 `json:"Unset"`
	} `json:"status_counter"`
}

// GetClusterManagerPeers queries the cluster manager for info about indexer cluster peers.
// You can only use this on a cluster manager.
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#cluster.2Fmanager.2Fpeers
func (c *SplunkClient) GetClusterManagerPeers() (map[string]ClusterManagerPeerInfo, error) {
	apiResponse := struct {
		Entry []struct {
			Name    string                 `json:"name"`
			Content ClusterManagerPeerInfo `json:"content"`
		} `json:"entry"`
	}{}
	path := "/services/cluster/manager/peers"
	err := c.Get(path, &apiResponse)
	if err != nil {
		return nil, err
	}

	peers := make(map[string]ClusterManagerPeerInfo)
	for _, e := range apiResponse.Entry {
		e.Content.ID = e.Name
		peers[e.Content.Label] = e.Content
	}

	return peers, nil
}

// RemoveIndexerClusterPeer removes peer from an indexer cluster, where id=unique GUID for the peer.
// You can only use this on a cluster manager.
// See https://docs.splunk.com/Documentation/Splunk/latest/Indexer/Removepeerfrommanagerlist
func (c *SplunkClient) RemoveIndexerClusterPeer(id string) error {
	// sent request to remove a peer from Cluster Manager peers list
	endpoint := fmt.Sprintf("%s%s?peers=%s", c.ManagementURI, "/services/cluster/manager/control/control/remove_peers", id)
	request, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}
	expectedStatus := []int{200}
	return c.Do(request, expectedStatus, nil)
}

// DecommissionIndexerClusterPeer takes an indexer cluster peer offline using the decommission endpoint.
// You can use this on any peer in an indexer cluster.
// See https://docs.splunk.com/Documentation/Splunk/latest/Indexer/Takeapeeroffline
func (c *SplunkClient) DecommissionIndexerClusterPeer(enforceCounts bool) error {
	enforceCountsAsInt := 0
	if enforceCounts {
		enforceCountsAsInt = 1
	}
	endpoint := fmt.Sprintf("%s%s?enforce_counts=%d", c.ManagementURI, "/services/cluster/peer/control/control/decommission", enforceCountsAsInt)
	request, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}
	expectedStatus := []int{200}
	return c.Do(request, expectedStatus, nil)
}

// BundlePush pushes the Cluster manager apps bundle to all the indexer peers
func (c *SplunkClient) BundlePush(ignoreIdenticalBundle bool) error {
	endpoint := fmt.Sprintf("%s%s", c.ManagementURI, "/services/cluster/manager/control/default/apply")
	reqBody := fmt.Sprintf("&ignore_identical_bundle=%t", ignoreIdenticalBundle)

	request, err := http.NewRequest("POST", endpoint, strings.NewReader(reqBody))
	if err != nil {
		return err
	}
	expectedStatus := []int{200}

	return c.Do(request, expectedStatus, nil)
}

//MCServerRolesInfo is the struct for the server roles of the localhost, in this case SplunkMonitoringConsole
type MCServerRolesInfo struct {
	ServerRoles []string `json:"server_roles"`
}

//MCDistributedPeers is the struct for information about distributed peers of the monitoring console
type MCDistributedPeers struct {
	ClusterLabel []string `json:"cluster_label"`
	ServerRoles  []string `json:"server_roles"`
}

//AutomateMCApplyChanges change the state of new indexers from "New" to "Configured" and add them in monitoring console asset table
func (c *SplunkClient) AutomateMCApplyChanges(mock bool) error {
	if mock {
		return nil
	}
	var configuredPeers, indexerMemberList, licenseManagerMemberList string
	apiResponseServerRoles, err := c.GetMonitoringconsoleServerRoles()
	if err != nil {
		return err
	}

	apiResponseMCDistributedPeers := struct {
		Entry []struct {
			Name    string             `json:"name"`
			Content MCDistributedPeers `json:"content"`
		} `json:"entry"`
	}{}
	path := "/services/search/distributed/peers"
	err = c.Get(path, &apiResponseMCDistributedPeers)
	if err != nil {
		return err
	}

	for _, e := range apiResponseMCDistributedPeers.Entry {
		if configuredPeers == "" {
			configuredPeers = e.Name
		} else {
			str := []string{configuredPeers, e.Name}
			configuredPeers = strings.Join(str, ",")
		}
		for _, s := range e.Content.ServerRoles {
			if s == "indexer" {
				indexerMemberList = indexerMemberList + "&member=" + e.Name
			}
			if s == "license_master" || s == "license_manager" {
				licenseManagerMemberList = licenseManagerMemberList + "&member=" + e.Name
			}
		}
	}

	for _, e := range apiResponseServerRoles.ServerRoles {
		if e == "indexer" {
			indexerMemberList = "&member=localhost:localhost" + indexerMemberList
		}
		if e == "license_master" || e == "license_manager" {
			licenseManagerMemberList = licenseManagerMemberList + "&member=localhost:localhost"
		}
	}
	reqBodyIndexer := indexerMemberList + "&default=true"
	reqBodyLicenseManager := licenseManagerMemberList + "&default=false"
	err = c.UpdateDMCGroups("dmc_group_indexer", reqBodyIndexer)
	if err != nil {
		return err
	}
	err = c.UpdateDMCGroups(splcommon.LicenseManagerDMCGroup, reqBodyLicenseManager)
	if err != nil {
		return err
	}

	clusterRoleDict := make(map[string][]string)
	//map of Name to Roles
	for _, e := range apiResponseMCDistributedPeers.Entry {
		clusterRoleDict[e.Name] = append(clusterRoleDict[e.Name], e.Content.ClusterLabel...)
	}
	//TODO: check different labels here
	clusterRoleDictToDict := make(map[string][]string)
	for key, value := range clusterRoleDict {
		for _, val := range value {
			clusterRoleDictToDict[val] = append(clusterRoleDictToDict[val], key)
		}
	}

	clusterRoleDictToDictString := make(map[string]string)
	for key, value := range clusterRoleDictToDict {
		for _, val := range value {
			clusterRoleDictToDictString[key] = clusterRoleDictToDictString[key] + "&member=" + val
		}
	}

	for key, value := range clusterRoleDictToDictString {
		if key == "" {
			break
		} else {
			err = c.UpdateDMCClusteringLabelGroup(key, value)
			if err != nil {
				return err
			}
		}
	}
	apiResponseMCAssetTableBuild, err := c.GetMonitoringconsoleAssetTable()
	if err != nil {
		return err
	}
	err = c.PostMonitoringConsoleAssetTable(apiResponseMCAssetTableBuild)
	if err != nil {
		return err
	}
	UISettingsObject, err := c.GetMonitoringConsoleUISettings()
	if err != nil {
		return err
	}
	err = c.UpdateLookupUISettings(configuredPeers, UISettingsObject)
	if err != nil {
		return err
	}
	err = c.UpdateMonitoringConsoleApp()
	if err != nil {
		return err
	}
	return err
}

//GetMonitoringconsoleServerRoles to retrive server roles of the local host or SplunkMonitoringConsole
func (c *SplunkClient) GetMonitoringconsoleServerRoles() (*MCServerRolesInfo, error) {
	apiResponseServerRoles := struct {
		Entry []struct {
			Content MCServerRolesInfo `json:"content"`
		} `json:"entry"`
	}{}
	path := "/services/server/info/server-info"
	err := c.Get(path, &apiResponseServerRoles)
	if err != nil {
		return nil, err
	}
	if len(apiResponseServerRoles.Entry) < 1 {
		return nil, fmt.Errorf("invalid response from %s%s", c.ManagementURI, path)
	}
	return &apiResponseServerRoles.Entry[0].Content, nil
}

//UpdateDMCGroups dmc* groups with new members
func (c *SplunkClient) UpdateDMCGroups(dmcGroupName string, groupMembers string) error {
	endpoint := fmt.Sprintf("%s/services/search/distributed/groups/%s/edit", c.ManagementURI, dmcGroupName)
	request, _ := http.NewRequest("POST", endpoint, strings.NewReader(groupMembers))
	expectedStatus := []int{200, 201, 409}
	err := c.Do(request, expectedStatus, nil)
	return err
}

//UpdateDMCClusteringLabelGroup update respective clustering group
func (c *SplunkClient) UpdateDMCClusteringLabelGroup(groupName string, groupMembers string) error {
	endpoint := fmt.Sprintf("%s/services/search/distributed/groups/dmc_indexerclustergroup_%s/edit", c.ManagementURI, groupName)
	reqBodyClusterGroup := groupMembers + "&default=false"
	request, _ := http.NewRequest("POST", endpoint, strings.NewReader(reqBodyClusterGroup))
	expectedStatus := []int{200, 201, 409}
	err := c.Do(request, expectedStatus, nil)
	return err
}

//MCAssetBuildTable is the struct for information about asset table
type MCAssetBuildTable struct {
	DispatchAutoCancel string `json:"dispatch.auto_cancel"`
	DispatchBuckets    int64  `json:"dispatch.buckets"`
}

//GetMonitoringconsoleAssetTable to GET monitoring console asset table data.
func (c *SplunkClient) GetMonitoringconsoleAssetTable() (*MCAssetBuildTable, error) {
	apiResponseMCAssetTableBuild := struct {
		Entry []struct {
			Content MCAssetBuildTable `json:"content"`
		} `json:"entry"`
	}{}
	path := "/servicesNS/nobody/splunk_monitoring_console/saved/searches/DMC%20Asset%20-%20Build%20Full"
	err := c.Get(path, &apiResponseMCAssetTableBuild)
	if err != nil {
		return nil, err
	}
	if len(apiResponseMCAssetTableBuild.Entry) < 1 {
		return nil, fmt.Errorf("invalid response from %s%s", c.ManagementURI, path)
	}
	return &apiResponseMCAssetTableBuild.Entry[0].Content, nil
}

//PostMonitoringConsoleAssetTable to build monitoring console asset table. Kicks off the search [Build Asset Table full]
func (c *SplunkClient) PostMonitoringConsoleAssetTable(apiResponseMCAssetTableBuild *MCAssetBuildTable) error {
	reqBodyAssetTable := "&trigger_actions=true&dispatch.auto_cancel=" + apiResponseMCAssetTableBuild.DispatchAutoCancel + "&dispatch.buckets=" + strconv.FormatInt(apiResponseMCAssetTableBuild.DispatchBuckets, 10) + "&dispatch.enablePreview=true"
	endpoint := c.ManagementURI + "/servicesNS/nobody/splunk_monitoring_console/saved/searches/DMC%20Asset%20-%20Build%20Full/dispatch"
	request, _ := http.NewRequest("POST", endpoint, strings.NewReader(reqBodyAssetTable))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	expectedStatus := []int{200, 201, 409}
	err := c.Do(request, expectedStatus, nil)
	return err
}

//UISettings is the struct for storing monitoring console app UI settings
type UISettings struct {
	EaiData     string `json:"eai:data"`
	Disabled    bool   `json:"disabled"`
	EaiACL      string `json:"eai:acl"`
	EaiAppName  string `json:"eai:appName"`
	EaiUserName string `json:"eai:userName"`
}

//GetMonitoringConsoleUISettings do a Get for app UI settings
func (c *SplunkClient) GetMonitoringConsoleUISettings() (*UISettings, error) {
	apiResponseUISettings := struct {
		Entry []struct {
			Content UISettings `json:"content"`
		} `json:"entry"`
	}{}
	path := "/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed"
	err := c.Get(path, &apiResponseUISettings)
	if err != nil {
		return nil, err
	}
	if len(apiResponseUISettings.Entry) < 1 {
		return nil, fmt.Errorf("invalid response from %s%s", c.ManagementURI, path)
	}
	return &apiResponseUISettings.Entry[0].Content, nil
}

//UpdateLookupUISettings updates assets.csv
func (c *SplunkClient) UpdateLookupUISettings(configuredPeers string, apiResponseUISettings *UISettings) error {
	reqBodyMCLookups := "configuredPeers=" + configuredPeers + "&eai:appName=" + apiResponseUISettings.EaiAppName + "&eai:acl=" + apiResponseUISettings.EaiACL + "&eai:userName=" + apiResponseUISettings.EaiUserName + "&disabled=" + strconv.FormatBool(apiResponseUISettings.Disabled)
	endpoint := fmt.Sprintf("%s/servicesNS/nobody/splunk_monitoring_console/configs/conf-splunk_monitoring_console_assets/settings", c.ManagementURI)
	request, _ := http.NewRequest("POST", endpoint, strings.NewReader(reqBodyMCLookups))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	expectedStatus := []int{200, 201, 409}
	err := c.Do(request, expectedStatus, nil)
	return err
}

//UpdateMonitoringConsoleApp updates the monitoring console app
func (c *SplunkClient) UpdateMonitoringConsoleApp() error {
	endpoint := fmt.Sprintf("%s/servicesNS/nobody/system/apps/local/splunk_monitoring_console", c.ManagementURI)
	request, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}
	expectedStatus := []int{200, 201}
	err = c.Do(request, expectedStatus, nil)
	return err
}

//ClusterInfo is the struct for checking ClusterInfo
type ClusterInfo struct {
	MultiSite             string `json:"multisite"`
	ReplicationFactor     int32  `json:"replication_factor"`
	SiteReplicationFactor string `json:"site_replication_factor,omitempty"`
}

// GetClusterInfo queries the cluster about multi-site or single-site.
//See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#cluster.2Fconfig
func (c *SplunkClient) GetClusterInfo(mockCall bool) (*ClusterInfo, error) {
	if mockCall {
		return nil, nil
	}
	apiResponse := struct {
		Entry []struct {
			Content ClusterInfo `json:"content"`
		} `json:"entry"`
	}{}
	path := "/services/cluster/config"
	err := c.Get(path, &apiResponse)
	if err != nil {
		return nil, err
	}
	if len(apiResponse.Entry) < 1 {
		return nil, fmt.Errorf("invalid response from %s%s", c.ManagementURI, path)
	}
	return &apiResponse.Entry[0].Content, nil
}

// SetIdxcSecret sets idxc_secret for a Splunk Instance
// Can be used on any peer in an indexer cluster as long as the idxc_secret matches the cluster manager
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#cluster.2Fconfig.2Fconfig
func (c *SplunkClient) SetIdxcSecret(idxcSecret string) error {
	endpoint := fmt.Sprintf("%s/services/cluster/config/config?secret=%s", c.ManagementURI, idxcSecret)
	request, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	expectedStatus := []int{200}
	return c.Do(request, expectedStatus, nil)
}

// RestartSplunk restarts specific Splunk instance
// Can be used for any Splunk Instance
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTsystem#server.2Fcontrol.2Frestart
func (c *SplunkClient) RestartSplunk() error {
	endpoint := fmt.Sprintf("%s/services/server/control/restart", c.ManagementURI)
	request, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}
	expectedStatus := []int{200}
	return c.Do(request, expectedStatus, nil)
}
