// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

package enterprise

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

// SplunkClient is a simple object used to send HTTP REST API requests
type SplunkClient struct {
	// https endpoint for management interface (e.g. "https://server:8089")
	managementURI string

	// username for authentication
	username string

	// password for authentication
	password string

	// HTTP client used to process requests
	client *http.Client
}

// NewSplunkClient returns a new SplunkClient object initialized with a username and password
func NewSplunkClient(managementURI, username, password string) *SplunkClient {
	return &SplunkClient{
		managementURI: managementURI,
		username:      username,
		password:      password,
		client: &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // don't verify ssl certs
		}},
	}
}

// Do processes a Splunk REST API request and unmarshals response into obj, if not nil
func (c *SplunkClient) Do(request *http.Request, expectedStatus int, obj interface{}) error {
	// send HTTP response and check status
	request.SetBasicAuth(c.username, c.password)
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	if response.StatusCode != expectedStatus {
		return fmt.Errorf("Response code=%d from %s; want %d", response.StatusCode, request.URL, expectedStatus)
	}
	if obj == nil {
		return nil
	}

	// unmarshall response if obj != nil
	data, _ := ioutil.ReadAll(response.Body)
	if len(data) == 0 {
		return fmt.Errorf("Received empty response body from %s", request.URL)
	}
	return json.Unmarshal(data, obj)
}

// Get sends a REST API request and unmarshals response into obj, if not nil
func (c *SplunkClient) Get(path string, obj interface{}) error {
	endpoint := fmt.Sprintf("%s%s?count=0&output_mode=json", c.managementURI, path)
	request, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return err
	}
	return c.Do(request, 200, obj)
}

// SearchHeadCaptainInfo represents the status of the search head cluster
// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#shcluster.2Fcaptain.2Finfo
type SearchHeadCaptainInfo struct {
	// Id of this SH cluster. This is used as the unique identifier for the Search Head Cluster in bundle replication and acceleration summary management.
	Identifier string `json:"id"`

	// Time when the current captain was elected
	ElectedCaptain int64 `json:"elected_captain"`

	// Indicates if the searchhead cluster is initialized.
	InitializedFlag bool `json:"initialized_flag"`

	// The name for the captain. Displayed on the Splunk Web manager page.
	Label string `json:"label"`

	// Indicates if the cluster is in maintenance mode.
	MaintenanceMode bool `json:"maintenance_mode"`

	// Flag to indicate if more then replication_factor peers have joined the cluster.
	MinPeersJoinedFlag bool `json:"min_peers_joined_flag"`

	// URI of the current captain.
	PeerSchemeHostPort string `json:"peer_scheme_host_port"`

	// Indicates whether the captain is restarting the members in a searchhead cluster.
	RollingRestartFlag bool `json:"rolling_restart_flag"`

	// Indicates whether the captain is ready to begin servicing, based on whether it is initialized.
	ServiceReadyFlag bool `json:"service_ready_flag"`

	// Timestamp corresponding to the creation of the captain.
	StartTime int64 `json:"start_time"`
}

// GetSearchHeadCaptainInfo queries the captain for info about the search head cluster
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
		return nil, fmt.Errorf("Invalid response from %s%s", c.managementURI, path)
	}
	return &apiResponse.Entry[0].Content, nil
}

// SearchHeadCaptainMemberInfo represents the status of a search head cluster member (captain endpoint)
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

	// Timestamp for last heartbeat recieved from the peer
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

// GetSearchHeadCaptainMembers queries the search head captain for info about cluster members
func (c *SplunkClient) GetSearchHeadCaptainMembers() (map[string]SearchHeadCaptainMemberInfo, error) {
	// query and parse
	// See https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#shcluster.2Fcaptain.2Fmembers
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

// SearchHeadClusterMemberInfo represents the status of a search head cluster member
// and https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTcluster#shcluster.2Fmember.2Finfo
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

// GetSearchHeadClusterMemberInfo queries info from a search head cluster member using /shcluster/member/info
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
		return nil, fmt.Errorf("Invalid response from %s%s", c.managementURI, path)
	}
	return &apiResponse.Entry[0].Content, nil
}

// SetSearchHeadDetention enables or disables detention of a search head cluster member
func (c *SplunkClient) SetSearchHeadDetention(detain bool) error {
	mode := "off"
	if detain {
		mode = "on"
	}
	endpoint := fmt.Sprintf("%s/services/shcluster/member/control/control/set_manual_detention?manual_detention=%s", c.managementURI, mode)
	request, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}
	return c.Do(request, 200, nil)
}

// RemoveSearchHeadClusterMember removes a search head cluster member
func (c *SplunkClient) RemoveSearchHeadClusterMember() error {
	endpoint := fmt.Sprintf("%s/services/shcluster/member/consensus/default/remove_server", c.managementURI)
	request, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		return err
	}
	return c.Do(request, 200, nil)
}
