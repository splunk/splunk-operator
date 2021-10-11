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

package client

import (
	"fmt"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"net/http"
	"strings"
	"testing"

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func splunkClientTester(t *testing.T, testMethod string, status int, body string, wantRequest *http.Request, test func(SplunkClient) error) {
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandler(wantRequest, status, body, nil)
	c := NewSplunkClient("https://localhost:8089", "admin", "p@ssw0rd")
	c.Client = mockSplunkClient
	err := test(*c)
	if err != nil {
		t.Errorf("%s err = %v", testMethod, err)
	}
	mockSplunkClient.CheckRequests(t, testMethod)
}

func splunkClientMultipleRequestTester(t *testing.T, testMethod string, status []int, body []string, wantRequest []*http.Request, test func(SplunkClient) error) {
	mockSplunkClient := &spltest.MockHTTPClient{}
	for i := 0; i < len(wantRequest); i++ {
		mockSplunkClient.AddHandler(wantRequest[i], status[i], body[i], nil)
	}
	c := NewSplunkClient("https://localhost:8089", "admin", "p@ssw0rd")
	c.Client = mockSplunkClient
	err := test(*c)
	if err != nil {
		t.Errorf("%s err = %v", testMethod, err)
	}
	mockSplunkClient.CheckRequests(t, testMethod)
}

func TestGetSearchHeadCaptainInfo(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/shcluster/captain/info?count=0&output_mode=json", nil)
	wantCaptainLabel := "splunk-s2-search-head-0"
	test := func(c SplunkClient) error {
		captainInfo, err := c.GetSearchHeadCaptainInfo()
		if err != nil {
			return err
		}
		if captainInfo.Label != wantCaptainLabel {
			t.Errorf("captainInfo.Label=%s; want %s", captainInfo.Label, wantCaptainLabel)
		}
		return nil
	}
	body := `{"links":{},"origin":"https://localhost:8089/services/shcluster/captain/info","updated":"2020-03-15T16:36:42+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"captain","id":"https://localhost:8089/services/shcluster/captain/info/captain","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/shcluster/captain/info/captain","list":"/services/shcluster/captain/info/captain"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"eai:acl":null,"elected_captain":1584139352,"id":"A9D5FCCF-EB93-4E0A-93E1-45B56483EA7A","initialized_flag":true,"label":"splunk-s2-search-head-0","maintenance_mode":false,"mgmt_uri":"https://splunk-s2-search-head-0.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089","min_peers_joined_flag":true,"peer_scheme_host_port":"https://splunk-s2-search-head-0.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089","rolling_restart_flag":false,"service_ready_flag":true,"start_time":1584139291}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`
	splunkClientTester(t, "TestGetSearchHeadCaptainInfo", 200, body, wantRequest, test)

	// test body with no entries
	test = func(c SplunkClient) error {
		_, err := c.GetSearchHeadCaptainInfo()
		if err == nil {
			t.Errorf("GetSearchHeadCaptainInfo returned nil; want error")
		}
		return nil
	}
	body = `{"links":{},"origin":"https://localhost:8089/services/shcluster/captain/info","updated":"2020-03-15T16:36:42+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[]}`
	splunkClientTester(t, "TestGetSearchHeadCaptainInfo", 200, body, wantRequest, test)

	// test empty body
	splunkClientTester(t, "TestGetSearchHeadCaptainInfo", 200, "", wantRequest, test)

	// test error code
	splunkClientTester(t, "TestGetSearchHeadCaptainInfo", 500, "", wantRequest, test)
}

func TestGetSearchHeadClusterMemberInfo(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/shcluster/member/info?count=0&output_mode=json", nil)
	wantMemberStatus := "Up"
	test := func(c SplunkClient) error {
		memberInfo, err := c.GetSearchHeadClusterMemberInfo()
		if err != nil {
			return err
		}
		if memberInfo.Status != wantMemberStatus {
			t.Errorf("memberInfo.Status=%s; want %s", memberInfo.Status, wantMemberStatus)
		}
		return nil
	}
	body := `{"links":{},"origin":"https://localhost:8089/services/shcluster/member/info","updated":"2020-03-15T16:30:38+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"member","id":"https://localhost:8089/services/shcluster/member/info/member","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/shcluster/member/info/member","list":"/services/shcluster/member/info/member"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"active_historical_search_count":0,"active_realtime_search_count":0,"adhoc_searchhead":false,"eai:acl":null,"is_registered":true,"last_heartbeat_attempt":1584289836,"maintenance_mode":false,"no_artifact_replications":false,"peer_load_stats_gla_15m":0,"peer_load_stats_gla_1m":0,"peer_load_stats_gla_5m":0,"peer_load_stats_max_runtime":0,"peer_load_stats_num_autosummary":0,"peer_load_stats_num_historical":0,"peer_load_stats_num_realtime":0,"peer_load_stats_num_running":0,"peer_load_stats_total_runtime":0,"restart_state":"NoRestart","status":"Up"}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`
	splunkClientTester(t, "TestGetSearchHeadClusterMemberInfo", 200, body, wantRequest, test)

	// test body with no entries
	test = func(c SplunkClient) error {
		_, err := c.GetSearchHeadClusterMemberInfo()
		if err == nil {
			t.Errorf("GetSearchHeadClusterMemberInfo returned nil; want error")
		}
		return nil
	}
	body = `{"links":{},"origin":"https://localhost:8089/services/shcluster/captain/info","updated":"2020-03-15T16:36:42+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[]}`
	splunkClientTester(t, "TestGetSearchHeadCaptainInfo", 200, body, wantRequest, test)

	// test empty body
	splunkClientTester(t, "TestGetSearchHeadClusterMemberInfo", 200, "", wantRequest, test)

	// test error code
	splunkClientTester(t, "TestGetSearchHeadClusterMemberInfo", 500, "", wantRequest, test)
}

func TestGetSearchHeadCaptainMembers(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/shcluster/captain/members?count=0&output_mode=json", nil)
	wantMembers := []string{
		"splunk-s2-search-head-0", "splunk-s2-search-head-1", "splunk-s2-search-head-2", "splunk-s2-search-head-3", "splunk-s2-search-head-4",
	}
	wantStatus := "Up"
	wantCaptain := "splunk-s2-search-head-0"
	test := func(c SplunkClient) error {
		members, err := c.GetSearchHeadCaptainMembers()
		if err != nil {
			return err
		}
		if len(members) != len(wantMembers) {
			t.Errorf("len(members)=%d; want %d", len(members), len(wantMembers))
		}
		for n := range wantMembers {
			member, ok := members[wantMembers[n]]
			if !ok {
				t.Errorf("wanted member not found: %s", wantMembers[n])
			}
			if member.Status != wantStatus {
				t.Errorf("member %s want Status=%s: got %s", wantMembers[n], member.Status, wantStatus)
			}
			if member.Captain {
				if wantMembers[n] != wantCaptain {
					t.Errorf("member %s want Captain=%t: got %t", wantMembers[n], false, true)
				}
			} else {
				if wantMembers[n] == wantCaptain {
					t.Errorf("member %s want Captain=%t: got %t", wantMembers[n], true, false)
				}
			}
		}
		return nil
	}
	body := `{"links":{"create":"/services/shcluster/captain/members/_new"},"origin":"https://localhost:8089/services/shcluster/captain/members","updated":"2020-03-15T16:40:20+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"7D571849-CD52-48F4-B76A-E83C4E86E300","id":"https://localhost:8089/services/shcluster/captain/members/7D571849-CD52-48F4-B76A-E83C4E86E300","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/shcluster/captain/members/7D571849-CD52-48F4-B76A-E83C4E86E300","list":"/services/shcluster/captain/members/7D571849-CD52-48F4-B76A-E83C4E86E300","edit":"/services/shcluster/captain/members/7D571849-CD52-48F4-B76A-E83C4E86E300"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"adhoc_searchhead":false,"advertise_restart_required":false,"artifact_count":2,"delayed_artifacts_to_discard":[],"eai:acl":null,"fixup_set":[],"host_port_pair":"10.42.0.3:8089","is_captain":false,"kv_store_host_port":"splunk-s2-search-head-2.splunk-s2-search-head-headless.splunk.svc.cluster.local:8191","label":"splunk-s2-search-head-2","last_heartbeat":1584290418,"mgmt_uri":"https://splunk-s2-search-head-2.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089","no_artifact_replications":false,"peer_scheme_host_port":"https://10.42.0.3:8089","pending_job_count":0,"preferred_captain":false,"replication_count":0,"replication_port":9887,"replication_use_ssl":false,"site":"default","status":"Up","status_counter":{"Complete":2,"NonStreamingTarget":0,"PendingDiscard":0}}},{"name":"90D7E074-9880-4867-BAA1-31A74EC28DC0","id":"https://localhost:8089/services/shcluster/captain/members/90D7E074-9880-4867-BAA1-31A74EC28DC0","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/shcluster/captain/members/90D7E074-9880-4867-BAA1-31A74EC28DC0","list":"/services/shcluster/captain/members/90D7E074-9880-4867-BAA1-31A74EC28DC0","edit":"/services/shcluster/captain/members/90D7E074-9880-4867-BAA1-31A74EC28DC0"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"adhoc_searchhead":false,"advertise_restart_required":false,"artifact_count":0,"delayed_artifacts_to_discard":[],"eai:acl":null,"fixup_set":[],"host_port_pair":"10.42.0.2:8089","is_captain":true,"kv_store_host_port":"splunk-s2-search-head-0.splunk-s2-search-head-headless.splunk.svc.cluster.local:8191","label":"splunk-s2-search-head-0","last_heartbeat":1584290416,"mgmt_uri":"https://splunk-s2-search-head-0.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089","no_artifact_replications":false,"peer_scheme_host_port":"https://10.42.0.2:8089","pending_job_count":0,"preferred_captain":true,"replication_count":0,"replication_port":9887,"replication_use_ssl":false,"site":"default","status":"Up","status_counter":{"Complete":0,"NonStreamingTarget":0,"PendingDiscard":0}}},{"name":"97B56FAE-E9C9-4B12-8B1E-A428E7859417","id":"https://localhost:8089/services/shcluster/captain/members/97B56FAE-E9C9-4B12-8B1E-A428E7859417","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/shcluster/captain/members/97B56FAE-E9C9-4B12-8B1E-A428E7859417","list":"/services/shcluster/captain/members/97B56FAE-E9C9-4B12-8B1E-A428E7859417","edit":"/services/shcluster/captain/members/97B56FAE-E9C9-4B12-8B1E-A428E7859417"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"adhoc_searchhead":false,"advertise_restart_required":false,"artifact_count":1,"delayed_artifacts_to_discard":[],"eai:acl":null,"fixup_set":[],"host_port_pair":"10.36.0.7:8089","is_captain":false,"kv_store_host_port":"splunk-s2-search-head-1.splunk-s2-search-head-headless.splunk.svc.cluster.local:8191","label":"splunk-s2-search-head-1","last_heartbeat":1584290418,"mgmt_uri":"https://splunk-s2-search-head-1.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089","no_artifact_replications":false,"peer_scheme_host_port":"https://10.36.0.7:8089","pending_job_count":0,"preferred_captain":false,"replication_count":0,"replication_port":9887,"replication_use_ssl":false,"site":"default","status":"Up","status_counter":{"Complete":1,"NonStreamingTarget":0,"PendingDiscard":0}}},{"name":"AA55C39A-5A3A-47CC-BF2C-2B60F0F6C561","id":"https://localhost:8089/services/shcluster/captain/members/AA55C39A-5A3A-47CC-BF2C-2B60F0F6C561","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/shcluster/captain/members/AA55C39A-5A3A-47CC-BF2C-2B60F0F6C561","list":"/services/shcluster/captain/members/AA55C39A-5A3A-47CC-BF2C-2B60F0F6C561","edit":"/services/shcluster/captain/members/AA55C39A-5A3A-47CC-BF2C-2B60F0F6C561"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"adhoc_searchhead":false,"advertise_restart_required":false,"artifact_count":1,"delayed_artifacts_to_discard":[],"eai:acl":null,"fixup_set":[],"host_port_pair":"10.42.0.5:8089","is_captain":false,"kv_store_host_port":"splunk-s2-search-head-4.splunk-s2-search-head-headless.splunk.svc.cluster.local:8191","label":"splunk-s2-search-head-4","last_heartbeat":1584290417,"mgmt_uri":"https://splunk-s2-search-head-4.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089","no_artifact_replications":false,"peer_scheme_host_port":"https://10.42.0.5:8089","pending_job_count":0,"preferred_captain":false,"replication_count":0,"replication_port":9887,"replication_use_ssl":false,"site":"default","status":"Up","status_counter":{"Complete":1,"NonStreamingTarget":0,"PendingDiscard":0}}},{"name":"E271B238-921F-4F6E-BD99-E110EB7B0FDA","id":"https://localhost:8089/services/shcluster/captain/members/E271B238-921F-4F6E-BD99-E110EB7B0FDA","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/shcluster/captain/members/E271B238-921F-4F6E-BD99-E110EB7B0FDA","list":"/services/shcluster/captain/members/E271B238-921F-4F6E-BD99-E110EB7B0FDA","edit":"/services/shcluster/captain/members/E271B238-921F-4F6E-BD99-E110EB7B0FDA"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"adhoc_searchhead":false,"advertise_restart_required":false,"artifact_count":2,"delayed_artifacts_to_discard":[],"eai:acl":null,"fixup_set":[],"host_port_pair":"10.40.0.4:8089","is_captain":false,"kv_store_host_port":"splunk-s2-search-head-3.splunk-s2-search-head-headless.splunk.svc.cluster.local:8191","label":"splunk-s2-search-head-3","last_heartbeat":1584290420,"mgmt_uri":"https://splunk-s2-search-head-3.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089","no_artifact_replications":false,"peer_scheme_host_port":"https://10.40.0.4:8089","pending_job_count":0,"preferred_captain":false,"replication_count":0,"replication_port":9887,"replication_use_ssl":false,"site":"default","status":"Up","status_counter":{"Complete":2,"NonStreamingTarget":0,"PendingDiscard":0}}}],"paging":{"total":5,"perPage":30,"offset":0},"messages":[]}`
	splunkClientTester(t, "TestGetSearchHeadCaptainMembers", 200, body, wantRequest, test)

	// test error response
	test = func(c SplunkClient) error {
		_, err := c.GetSearchHeadCaptainMembers()
		if err == nil {
			t.Errorf("GetSearchHeadCaptainMembers returned nil; want error")
		}
		return nil
	}
	splunkClientTester(t, "TestGetSearchHeadCaptainMembers", 503, "", wantRequest, test)
}

func TestSetSearchHeadDetention(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/shcluster/member/control/control/set_manual_detention?manual_detention=on", nil)
	test := func(c SplunkClient) error {
		return c.SetSearchHeadDetention(true)
	}
	splunkClientTester(t, "TestSetSearchHeadDetention", 200, "", wantRequest, test)
}

func TestBundlePush(t *testing.T) {
	body := strings.NewReader("&ignore_identical_bundle=true")
	wantRequest, _ := http.NewRequest("POST", splcommon.ApplyBundleTest, body)

	test := func(c SplunkClient) error {
		return c.BundlePush(true)
	}
	splunkClientTester(t, "TestBundlePush", 200, "", wantRequest, test)
}

func TestRemoveSearchHeadClusterMember(t *testing.T) {
	// test for 200 response first (sent on first removal request)
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/shcluster/member/consensus/default/remove_server?output_mode=json", nil)
	test := func(c SplunkClient) error {
		return c.RemoveSearchHeadClusterMember()
	}
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 200, "", wantRequest, test)

	// next test 503 error message (sent for short period after removal, while SH is updating itself)
	body := `{"messages":[{"type":"ERROR","text":"Failed to proxy call to member https://splunk-s2-search-head-0.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089. ERROR:  Server https://splunk-s2-search-head-3.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089 is not part of configuration, hence cannot be removed. Check configuration by making GET request onto /services/shcluster/member/consensus"}]}`
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 503, body, wantRequest, test)

	// check alternate 503 message (sent after SH has completed removal)
	body = `{"messages":[{"type":"ERROR","text":"This node is not part of any cluster configuration, please re-run the command from an active cluster member. Also see \"splunk add shcluster-member\" to add this member to an existing cluster or see \"splunk bootstrap shcluster-captain\" to bootstrap a new cluster with this member."}]}`
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 503, body, wantRequest, test)

	// test unrecognized response message
	test = func(c SplunkClient) error {
		err := c.RemoveSearchHeadClusterMember()
		if err == nil {
			t.Errorf("RemoveSearchHeadClusterMember returned nil; want error")
		}
		return nil
	}
	body = `{"messages":[{"type":"ERROR","text":"Nothing that we are expecting."}]}`
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 503, body, wantRequest, test)

	// test empty messages array in response
	body = `{"messages":[]}`
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 503, body, wantRequest, test)

	// test unmarshal failure
	body = `<invalid>`
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 503, body, wantRequest, test)

	// test empty response
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 503, "", wantRequest, test)

	// test bad response code
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 404, "", wantRequest, test)
}

func TestGetClusterMasterInfo(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", splcommon.ManagerInfoTest, nil)
	wantInfo := ClusterMasterInfo{
		Initialized:     true,
		IndexingReady:   true,
		ServiceReady:    true,
		MaintenanceMode: false,
		RollingRestart:  false,
		Label:           splcommon.SplunkS1ClusterManagerZero,
		ActiveBundle: ClusterBundleInfo{
			BundlePath: "/opt/splunk/var/run/splunk/cluster/remote-bundle/506c58d5aeda1dd6017889e3186e7337-1583870198.bundle",
			Checksum:   "14310A4AABD23E85BBD4559C4A3B59F8",
			Timestamp:  1583870198,
		},
		LatestBundle: ClusterBundleInfo{
			BundlePath: "/opt/splunk/var/run/splunk/cluster/remote-bundle/506c58d5aeda1dd6017889e3186e7337-1583870198.bundle",
			Checksum:   "14310A4AABD23E85BBD4559C4A3B59F8",
			Timestamp:  1583870198,
		},
		StartTime: 1583948636,
	}
	test := func(c SplunkClient) error {
		gotInfo, err := c.GetClusterMasterInfo()
		if err != nil {
			return err
		}
		if *gotInfo != wantInfo {
			t.Errorf("info.Status=%v; want %v", *gotInfo, wantInfo)
		}
		return nil
	}
	body := splcommon.BodyTestGetCMInfo
	splunkClientTester(t, "TestGetClusterMasterInfo", 200, body, wantRequest, test)

	// test body with no entries
	test = func(c SplunkClient) error {
		_, err := c.GetClusterMasterInfo()
		if err == nil {
			t.Errorf("GetClusterMasterInfo returned nil; want error")
		}
		return nil
	}
	body = splcommon.BodyTestGetCMInfoEmpty
	splunkClientTester(t, "TestGetClusterMasterInfo", 200, body, wantRequest, test)

	// test error code
	splunkClientTester(t, "TestGetClusterMasterInfo", 500, "", wantRequest, test)
}

func TestGetIndexerClusterPeerInfo(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", splcommon.PeerInfoJSON, nil)
	wantMemberStatus := "Up"
	test := func(c SplunkClient) error {
		info, err := c.GetIndexerClusterPeerInfo()
		if err != nil {
			return err
		}
		if info.Status != wantMemberStatus {
			t.Errorf("info.Status=%s; want %s", info.Status, wantMemberStatus)
		}
		return nil
	}
	body := splcommon.BodyTestGetIndexerClusterPeerInfo
	splunkClientTester(t, "TestGetIndexerClusterPeerInfo", 200, body, wantRequest, test)

	// test body with no entries
	test = func(c SplunkClient) error {
		_, err := c.GetIndexerClusterPeerInfo()
		if err == nil {
			t.Errorf("GetIndexerClusterPeerInfo returned nil; want error")
		}
		return nil
	}
	body = splcommon.BodyTestGetIndexerClusterPeerInfoEmpty
	splunkClientTester(t, "TestGetIndexerClusterPeerInfo", 200, body, wantRequest, test)

	// test error code
	splunkClientTester(t, "TestGetIndexerClusterPeerInfo", 500, "", wantRequest, test)
}

func TestGetClusterMasterPeers(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", splcommon.ManagerPeersTest, nil)
	var wantPeers = []struct {
		ID     string
		Label  string
		Status string
	}{
		{ID: "D39B1729-E2C5-4273-B9B2-534DA7C2F866", Label: "splunk-s1-indexer-0", Status: "Up"},
	}
	test := func(c SplunkClient) error {
		peers, err := c.GetClusterMasterPeers()
		if err != nil {
			return err
		}
		if len(peers) != len(wantPeers) {
			t.Errorf("len(peers)=%d; want %d", len(peers), len(wantPeers))
		}
		for n := range wantPeers {
			p, ok := peers[wantPeers[n].Label]
			if !ok {
				t.Errorf("wanted peer not found: %s", wantPeers[n].Label)
			}
			if p.ID != wantPeers[n].ID {
				t.Errorf("peer %s want ID=%s: got %s", wantPeers[n].Label, p.ID, wantPeers[n].ID)
			}
			if p.Label != wantPeers[n].Label {
				t.Errorf("peer %s want Label=%s: got %s", wantPeers[n].Label, p.Label, wantPeers[n].Label)
			}
			if p.Status != wantPeers[n].Status {
				t.Errorf("peer %s want Status=%s: got %s", wantPeers[n].Label, p.Status, wantPeers[n].Status)
			}
		}
		return nil
	}
	body := splcommon.BodyTestGetCMPeers
	splunkClientTester(t, "TestGetClusterMasterPeers", 200, body, wantRequest, test)

	// test error response
	test = func(c SplunkClient) error {
		_, err := c.GetClusterMasterPeers()
		if err == nil {
			t.Errorf("GetClusterMasterPeers returned nil; want error")
		}
		return nil
	}
	splunkClientTester(t, "TestGetClusterMasterPeers", 503, "", wantRequest, test)
}

func TestRemoveIndexerClusterPeer(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", splcommon.RemovePeersTest+"peers=D39B1729-E2C5-4273-B9B2-534DA7C2F866", nil)
	test := func(c SplunkClient) error {
		return c.RemoveIndexerClusterPeer("D39B1729-E2C5-4273-B9B2-534DA7C2F866")
	}
	splunkClientTester(t, "TestRemoveIndexerClusterPeer", 200, "", wantRequest, test)
}

func TestDecommissionIndexerClusterPeer(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", splcommon.PeerDecommission+"enforce_counts=1", nil)
	test := func(c SplunkClient) error {
		return c.DecommissionIndexerClusterPeer(true)
	}
	splunkClientTester(t, "TestDecommissionIndexerClusterPeer", 200, "", wantRequest, test)
}

func TestAutomateMCApplyChanges(t *testing.T) {
	request1, _ := http.NewRequest("GET", "https://localhost:8089/services/server/info/server-info?count=0&output_mode=json", nil)
	request2, _ := http.NewRequest("GET", "https://localhost:8089/services/search/distributed/peers?count=0&output_mode=json", nil)
	request3, _ := http.NewRequest("POST", "https://localhost:8089/services/search/distributed/groups/dmc_group_indexer/edit", nil)
	request4, _ := http.NewRequest("POST", "https://localhost:8089/services/search/distributed/groups/dmc_group_license_master/edit", nil)
	request5, _ := http.NewRequest("POST", "https://localhost:8089/services/search/distributed/groups/dmc_indexerclustergroup_idxc_label/edit", nil)
	request6, _ := http.NewRequest("GET", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/saved/searches/DMC%20Asset%20-%20Build%20Full?count=0&output_mode=json", nil)
	request7, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/saved/searches/DMC%20Asset%20-%20Build%20Full/dispatch", nil)
	request8, _ := http.NewRequest("GET", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed?count=0&output_mode=json", nil)
	request9, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/configs/conf-splunk_monitoring_console_assets/settings", nil)
	request10, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/system/apps/local/splunk_monitoring_console", nil)
	var wantRequests []*http.Request
	wantRequests = []*http.Request(append(wantRequests, request1, request2, request3, request4, request5, request6, request7, request8, request9, request10))
	body := []string{
		`{"links":{},"origin":"https://localhost:8089/services/server/info","updated":"2020-09-24T06:38:53+00:00","generator":{"build":"a1a6394cc5ae","version":"8.0.5"},"entry":[{"name":"server-info","id":"https://localhost:8089/services/server/info/server-info","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/server/info/server-info","list":"/services/server/info/server-info"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["*"],"write":[]},"removable":false,"sharing":"system"},"fields":{"required":[],"optional":[],"wildcard":[]},"content":{"activeLicenseGroup":"Trial","activeLicenseSubgroup":"Production","build":"a1a6394cc5ae","cluster_label":["idxc_label"],"cpu_arch":"x86_64","eai:acl":null,"fips_mode":false,"guid":"0F93F33C-4BDA-4A74-AD9F-3FCE26C6AFF0","health_info":"green","health_version":1,"host":"splunk-default-monitoring-console-86bc9b7c8c-d96x2","host_fqdn":"splunk-default-monitoring-console-86bc9b7c8c-d96x2","host_resolved":"splunk-default-monitoring-console-86bc9b7c8c-d96x2","isForwarding":true,"isFree":false,"isTrial":true,"kvStoreStatus":"ready","licenseKeys":["5C52DA5145AD67B8188604C49962D12F2C3B2CF1B82A6878E46F68CA2812807B"],"licenseSignature":"139bf73ec92c84121c79a9b8307a6724","licenseState":"OK","license_labels":["Splunk Enterprise   Splunk Analytics for Hadoop Download Trial"],"master_guid":"0F93F33C-4BDA-4A74-AD9F-3FCE26C6AFF0","master_uri":"self","max_users":4294967295,"mode":"normal","numberOfCores":1,"numberOfVirtualCores":2,"os_build":"#1 SMP Thu Sep 3 19:04:44 UTC 2020","os_name":"Linux","os_name_extended":"Linux","os_version":"4.14.193-149.317.amzn2.x86_64","physicalMemoryMB":7764,"product_type":"enterprise","rtsearch_enabled":true,"serverName":"splunk-default-monitoring-console-86bc9b7c8c-d96x2","server_roles":["license_master","cluster_search_head","search_head"],"startup_time":1600928786,"staticAssetId":"CFE3D41EE2CCD1465E8C8453F83E4ECFFF540780B4490E84458DD4A3694CE4D1","version":"8.0.5"}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`,
		splcommon.TestMCApplyChanges,
		"",
		"",
		"",
		`{"links":{"create":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/_new","_reload":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/_reload","_acl":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/_acl"},"origin":"https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/data/ui/nav","updated":"2020-09-23T23:50:25+00:00","generator":{"build":"a1a6394cc5ae","version":"8.0.5"},"entry":[{"name":"default.distributed","id":"https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed","list":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed","_reload":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed/_reload","edit":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed"},"author":"nobody","acl":{"app":"splunk_monitoring_console","can_change_perms":true,"can_list":true,"can_share_app":true,"can_share_global":true,"can_share_user":false,"can_write":true,"modifiable":true,"owner":"nobody","perms":{"read":["admin"],"write":["admin"]},"removable":false,"sharing":"app"},"fields":{"required":["eai:data"],"optional":[],"wildcard":[]},"content":{"disabled":false,"eai:acl":null,"eai:appName":"splunk_monitoring_console","eai:data":"<nav color=\"#3C444D\">\n  <view name=\"monitoringconsole_overview\" default=\"true\" />\n  <view name=\"monitoringconsole_landing\" />\n  <view name=\"monitoringconsole_check\" />\n  <view name=\"monitoringconsole_instances\" />\n  <collection label=\"Indexing\">\n    <collection label=\"Performance\">\n      <view name=\"indexing_performance_instance\" />\n      <view name=\"indexing_performance_advanced\" />\n      <view name=\"indexing_performance_deployment\" />\n    <\/collection>\n    <collection label=\"Indexer Clustering\">\n        <!--<a href=\"Clustering\">Indexer Clustering: Status<\/a>-->\n      <view name=\"indexer_clustering_status\" />\n      <view name=\"indexer_clustering_service_activity\" />\n    <\/collection>\n    <collection label=\"Indexes and Volumes\">\n      <view name=\"indexes_and_volumes_instance\" />\n      <view name=\"indexes_and_volumes_deployment\" />\n      <view name=\"index_detail_instance\" />\n      <view name=\"index_detail_deployment\" />\n      <view name=\"volume_detail_instance\" />\n      <view name=\"volume_detail_deployment\" />\n    <\/collection>\n    <collection label=\"Inputs\">\n      <view name=\"http_event_collector_instance\" />\n      <view name=\"http_event_collector_deployment\" />\n      <view name=\"splunk_tcpin_performance_instance\" />\n      <view name=\"splunk_tcpin_performance_deployment\" />\n      <view name=\"data_quality\" />\n    <\/collection>\n    <collection label=\"License Usage\">\n      <view name=\"license_usage_today\" />\n      <view name=\"license_usage_30days\" />\n    <\/collection>\n    <collection label=\"SmartStore\">\n      <view name=\"smartstore_activity_instance\" />\n      <view name=\"smartstore_activity_deployment\" />\n      <view name=\"smartstore_cache_performance_instance\" />\n      <view name=\"smartstore_cache_performance_deployment\" />\n    <\/collection>\n  <\/collection>\n  <collection label=\"Search\">\n    <collection label=\"Activity\">\n      <view name=\"search_activity_instance\" />\n      <view name=\"search_activity_deployment\" />\n      <view name=\"search_usage_statistics_instance\" />\n      <view name=\"search_usage_statistics_deployment\" />\n    <\/collection>\n    <collection label=\"Distributed Search\">\n      <view name=\"distributed_search_instance\" />\n      <view name=\"distributed_search_deployment\" />\n    <\/collection>\n    <collection label=\"Search Head Clustering\">\n      <view name=\"shc_status_and_conf\" />\n      <view name=\"shc_conf_rep\" />\n      <view name=\"shc_artifact_replication\" />\n      <view name=\"shc_scheduler_delegation_statistics\" />\n      <view name=\"shc_app_deployment\" />\n    <\/collection>\n    <collection label=\"Scheduler Activity\">\n      <view name=\"scheduler_activity_instance\" />\n      <view name=\"scheduler_activity_deployment\" />\n    <\/collection>\n    <collection label=\"KV Store\">\n      <view name=\"kv_store_instance\" />\n      <view name=\"kv_store_deployment\" />\n    <\/collection>\n    <collection label=\"Knowledge Bundle Replication\">\n      <view name=\"bundle_replication\" />\n      <view name=\"cascading_replication\" />\n    <\/collection>\n  <\/collection>\n  <collection label=\"Resource Usage\">\n    <view name=\"resource_usage_instance\" />\n    <view name=\"resource_usage_machine\" />\n    <view name=\"resource_usage_deployment\" />\n    <view name=\"resource_usage_cpu_instance\" />\n    <view name=\"resource_usage_cpu_deployment\" />\n    <collection label=\"Workload Management\">\n      <view name=\"workload_management\" />\n      <view name=\"workload_management_per_pool_instance\" />\n      <view name=\"workload_management_per_pool_deployment\" />\n    <\/collection>\n  <\/collection>\n  <collection label=\"Forwarders\">\n    <view name=\"forwarder_instance\" />\n    <view name=\"forwarder_deployment\" />\n  <\/collection>\n  <collection label=\"Settings\">\n    <view name=\"monitoringconsole_configure\" />\n    <view name=\"monitoringconsole_forwarder_setup\" />\n    <view name=\"monitoringconsole_alerts_setup\" />\n    <view name=\"monitoringconsole_overview_preferences\"/>\n    <view name=\"monitoringconsole_check_list\" />\n  <\/collection>\n  <a href=\"search\">Run a Search<\/a>\n<\/nav>","eai:digest":"31adb23c29a2d1402e5867ee6b9b5a92","eai:userName":"nobody","rootNode":"nav"}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`,
		"",
		`{"links":{"create":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/_new","_reload":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/_reload","_acl":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/_acl"},"origin":"https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/data/ui/nav","updated":"2020-09-23T23:50:25+00:00","generator":{"build":"a1a6394cc5ae","version":"8.0.5"},"entry":[{"name":"default.distributed","id":"https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed","list":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed","_reload":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed/_reload","edit":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed"},"author":"nobody","acl":{"app":"splunk_monitoring_console","can_change_perms":true,"can_list":true,"can_share_app":true,"can_share_global":true,"can_share_user":false,"can_write":true,"modifiable":true,"owner":"nobody","perms":{"read":["admin"],"write":["admin"]},"removable":false,"sharing":"app"},"fields":{"required":["eai:data"],"optional":[],"wildcard":[]},"content":{"disabled":false,"eai:acl":null,"eai:appName":"splunk_monitoring_console","eai:data":"<nav color=\"#3C444D\">\n  <view name=\"monitoringconsole_overview\" default=\"true\" />\n  <view name=\"monitoringconsole_landing\" />\n  <view name=\"monitoringconsole_check\" />\n  <view name=\"monitoringconsole_instances\" />\n  <collection label=\"Indexing\">\n    <collection label=\"Performance\">\n      <view name=\"indexing_performance_instance\" />\n      <view name=\"indexing_performance_advanced\" />\n      <view name=\"indexing_performance_deployment\" />\n    <\/collection>\n    <collection label=\"Indexer Clustering\">\n        <!--<a href=\"Clustering\">Indexer Clustering: Status<\/a>-->\n      <view name=\"indexer_clustering_status\" />\n      <view name=\"indexer_clustering_service_activity\" />\n    <\/collection>\n    <collection label=\"Indexes and Volumes\">\n      <view name=\"indexes_and_volumes_instance\" />\n      <view name=\"indexes_and_volumes_deployment\" />\n      <view name=\"index_detail_instance\" />\n      <view name=\"index_detail_deployment\" />\n      <view name=\"volume_detail_instance\" />\n      <view name=\"volume_detail_deployment\" />\n    <\/collection>\n    <collection label=\"Inputs\">\n      <view name=\"http_event_collector_instance\" />\n      <view name=\"http_event_collector_deployment\" />\n      <view name=\"splunk_tcpin_performance_instance\" />\n      <view name=\"splunk_tcpin_performance_deployment\" />\n      <view name=\"data_quality\" />\n    <\/collection>\n    <collection label=\"License Usage\">\n      <view name=\"license_usage_today\" />\n      <view name=\"license_usage_30days\" />\n    <\/collection>\n    <collection label=\"SmartStore\">\n      <view name=\"smartstore_activity_instance\" />\n      <view name=\"smartstore_activity_deployment\" />\n      <view name=\"smartstore_cache_performance_instance\" />\n      <view name=\"smartstore_cache_performance_deployment\" />\n    <\/collection>\n  <\/collection>\n  <collection label=\"Search\">\n    <collection label=\"Activity\">\n      <view name=\"search_activity_instance\" />\n      <view name=\"search_activity_deployment\" />\n      <view name=\"search_usage_statistics_instance\" />\n      <view name=\"search_usage_statistics_deployment\" />\n    <\/collection>\n    <collection label=\"Distributed Search\">\n      <view name=\"distributed_search_instance\" />\n      <view name=\"distributed_search_deployment\" />\n    <\/collection>\n    <collection label=\"Search Head Clustering\">\n      <view name=\"shc_status_and_conf\" />\n      <view name=\"shc_conf_rep\" />\n      <view name=\"shc_artifact_replication\" />\n      <view name=\"shc_scheduler_delegation_statistics\" />\n      <view name=\"shc_app_deployment\" />\n    <\/collection>\n    <collection label=\"Scheduler Activity\">\n      <view name=\"scheduler_activity_instance\" />\n      <view name=\"scheduler_activity_deployment\" />\n    <\/collection>\n    <collection label=\"KV Store\">\n      <view name=\"kv_store_instance\" />\n      <view name=\"kv_store_deployment\" />\n    <\/collection>\n    <collection label=\"Knowledge Bundle Replication\">\n      <view name=\"bundle_replication\" />\n      <view name=\"cascading_replication\" />\n    <\/collection>\n  <\/collection>\n  <collection label=\"Resource Usage\">\n    <view name=\"resource_usage_instance\" />\n    <view name=\"resource_usage_machine\" />\n    <view name=\"resource_usage_deployment\" />\n    <view name=\"resource_usage_cpu_instance\" />\n    <view name=\"resource_usage_cpu_deployment\" />\n    <collection label=\"Workload Management\">\n      <view name=\"workload_management\" />\n      <view name=\"workload_management_per_pool_instance\" />\n      <view name=\"workload_management_per_pool_deployment\" />\n    <\/collection>\n  <\/collection>\n  <collection label=\"Forwarders\">\n    <view name=\"forwarder_instance\" />\n    <view name=\"forwarder_deployment\" />\n  <\/collection>\n  <collection label=\"Settings\">\n    <view name=\"monitoringconsole_configure\" />\n    <view name=\"monitoringconsole_forwarder_setup\" />\n    <view name=\"monitoringconsole_alerts_setup\" />\n    <view name=\"monitoringconsole_overview_preferences\"/>\n    <view name=\"monitoringconsole_check_list\" />\n  <\/collection>\n  <a href=\"search\">Run a Search<\/a>\n<\/nav>","eai:digest":"31adb23c29a2d1402e5867ee6b9b5a92","eai:userName":"nobody","rootNode":"nav"}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`,
		"",
		"",
	}
	test := func(c SplunkClient) error {
		return c.AutomateMCApplyChanges(false)
	}
	status := []int{
		200, 200, 200, 200, 200, 200, 201, 200, 200, 200, 200,
	}
	splunkClientMultipleRequestTester(t, "TestAutomateMCApplyChanges", status, body, wantRequests, test)
}
func TestGetMonitoringconsoleServerRoles(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/server/info/server-info?count=0&output_mode=json", nil)
	test := func(c SplunkClient) error {
		info, err := c.GetMonitoringconsoleServerRoles()
		if err != nil {
			return err
		}
		if len(info.ServerRoles) == 0 {
			t.Errorf("There should be atleast one server role assigned to this host")
		}
		return nil
	}
	body := splcommon.BodyTestGetClusterInfo
	splunkClientTester(t, "TestGetMonitoringconsoleServerRoles", 200, body, wantRequest, test)
}
func TestUpdateDMCGroups(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/search/distributed/groups/indexer/edit", nil)
	test := func(c SplunkClient) error {
		err := c.UpdateDMCGroups("indexer", "splunk_cluster_master")
		if err != nil {
			t.Errorf("Unable to update monitoring console clustering groups")
		}
		return nil
	}
	splunkClientTester(t, "TestUpdateDMCGroups", 201, "", wantRequest, test)
}
func TestUpdateDMCClusteringLabelGroup(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/search/distributed/groups/dmc_indexerclustergroup_abc/edit", nil)
	test := func(c SplunkClient) error {
		err := c.UpdateDMCClusteringLabelGroup("abc", "splunk_cluster_master")
		if err != nil {
			t.Errorf("Unable to update monitoring console clustering groups")
		}
		return nil
	}
	splunkClientTester(t, "TestUpdateDMCClusteringLabelGroup", 201, "", wantRequest, test)
}

func TestGetMonitoringconsoleAssetTable(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/saved/searches/DMC%20Asset%20-%20Build%20Full?count=0&output_mode=json", nil)
	wantDispatchBuckets := int64(0)
	test := func(c SplunkClient) error {
		info, err := c.GetMonitoringconsoleAssetTable()
		if err != nil {
			return err
		}
		if info.DispatchBuckets != wantDispatchBuckets {
			t.Errorf("info.Status=%d; want %d", info.DispatchBuckets, wantDispatchBuckets)
		}
		return nil
	}
	body := splcommon.BodyTestGetMCAssetTable
	splunkClientTester(t, "TestGetMonitoringconsoleAssetTable", 200, body, wantRequest, test)
}

func TestPostMonitoringConsoleAssetTable(t *testing.T) {
	apiResponseMCAssetBuild := new(MCAssetBuildTable)
	apiResponseMCAssetBuild = &MCAssetBuildTable{
		DispatchAutoCancel: "30",
		DispatchBuckets:    int64(0),
	}

	body := strings.NewReader("output_mode=json&trigger_actions=true&dispatch.auto_cancel=30&dispatch.buckets=300&dispatch.enablePreview=true")
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/saved/searches/DMC%20Asset%20-%20Build%20Full/dispatch", body)
	wantRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	test := func(c SplunkClient) error {
		return c.PostMonitoringConsoleAssetTable(apiResponseMCAssetBuild)
	}
	splunkClientTester(t, "TestPostMonitoringConsoleAssetTable", 201, "", wantRequest, test)
}

func TestGetMonitoringConsoleUISettings(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed?count=0&output_mode=json", nil)
	wantEaiAppName := "splunk_monitoring_console"
	test := func(c SplunkClient) error {
		info, err := c.GetMonitoringConsoleUISettings()
		if err != nil {
			return err
		}
		if info.EaiAppName != wantEaiAppName {
			t.Errorf("info.Status=%s; want %s", info.EaiAppName, wantEaiAppName)
		}
		return nil
	}
	body := `{"links":{"create":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/_new","_reload":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/_reload","_acl":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/_acl"},"origin":"https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/data/ui/nav","updated":"2020-09-23T23:50:25+00:00","generator":{"build":"a1a6394cc5ae","version":"8.0.5"},"entry":[{"name":"default.distributed","id":"https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed","list":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed","_reload":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed/_reload","edit":"/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed"},"author":"nobody","acl":{"app":"splunk_monitoring_console","can_change_perms":true,"can_list":true,"can_share_app":true,"can_share_global":true,"can_share_user":false,"can_write":true,"modifiable":true,"owner":"nobody","perms":{"read":["admin"],"write":["admin"]},"removable":false,"sharing":"app"},"fields":{"required":["eai:data"],"optional":[],"wildcard":[]},"content":{"disabled":false,"eai:acl":null,"eai:appName":"splunk_monitoring_console","eai:data":"<nav color=\"#3C444D\">\n  <view name=\"monitoringconsole_overview\" default=\"true\" />\n  <view name=\"monitoringconsole_landing\" />\n  <view name=\"monitoringconsole_check\" />\n  <view name=\"monitoringconsole_instances\" />\n  <collection label=\"Indexing\">\n    <collection label=\"Performance\">\n      <view name=\"indexing_performance_instance\" />\n      <view name=\"indexing_performance_advanced\" />\n      <view name=\"indexing_performance_deployment\" />\n    <\/collection>\n    <collection label=\"Indexer Clustering\">\n        <!--<a href=\"Clustering\">Indexer Clustering: Status<\/a>-->\n      <view name=\"indexer_clustering_status\" />\n      <view name=\"indexer_clustering_service_activity\" />\n    <\/collection>\n    <collection label=\"Indexes and Volumes\">\n      <view name=\"indexes_and_volumes_instance\" />\n      <view name=\"indexes_and_volumes_deployment\" />\n      <view name=\"index_detail_instance\" />\n      <view name=\"index_detail_deployment\" />\n      <view name=\"volume_detail_instance\" />\n      <view name=\"volume_detail_deployment\" />\n    <\/collection>\n    <collection label=\"Inputs\">\n      <view name=\"http_event_collector_instance\" />\n      <view name=\"http_event_collector_deployment\" />\n      <view name=\"splunk_tcpin_performance_instance\" />\n      <view name=\"splunk_tcpin_performance_deployment\" />\n      <view name=\"data_quality\" />\n    <\/collection>\n    <collection label=\"License Usage\">\n      <view name=\"license_usage_today\" />\n      <view name=\"license_usage_30days\" />\n    <\/collection>\n    <collection label=\"SmartStore\">\n      <view name=\"smartstore_activity_instance\" />\n      <view name=\"smartstore_activity_deployment\" />\n      <view name=\"smartstore_cache_performance_instance\" />\n      <view name=\"smartstore_cache_performance_deployment\" />\n    <\/collection>\n  <\/collection>\n  <collection label=\"Search\">\n    <collection label=\"Activity\">\n      <view name=\"search_activity_instance\" />\n      <view name=\"search_activity_deployment\" />\n      <view name=\"search_usage_statistics_instance\" />\n      <view name=\"search_usage_statistics_deployment\" />\n    <\/collection>\n    <collection label=\"Distributed Search\">\n      <view name=\"distributed_search_instance\" />\n      <view name=\"distributed_search_deployment\" />\n    <\/collection>\n    <collection label=\"Search Head Clustering\">\n      <view name=\"shc_status_and_conf\" />\n      <view name=\"shc_conf_rep\" />\n      <view name=\"shc_artifact_replication\" />\n      <view name=\"shc_scheduler_delegation_statistics\" />\n      <view name=\"shc_app_deployment\" />\n    <\/collection>\n    <collection label=\"Scheduler Activity\">\n      <view name=\"scheduler_activity_instance\" />\n      <view name=\"scheduler_activity_deployment\" />\n    <\/collection>\n    <collection label=\"KV Store\">\n      <view name=\"kv_store_instance\" />\n      <view name=\"kv_store_deployment\" />\n    <\/collection>\n    <collection label=\"Knowledge Bundle Replication\">\n      <view name=\"bundle_replication\" />\n      <view name=\"cascading_replication\" />\n    <\/collection>\n  <\/collection>\n  <collection label=\"Resource Usage\">\n    <view name=\"resource_usage_instance\" />\n    <view name=\"resource_usage_machine\" />\n    <view name=\"resource_usage_deployment\" />\n    <view name=\"resource_usage_cpu_instance\" />\n    <view name=\"resource_usage_cpu_deployment\" />\n    <collection label=\"Workload Management\">\n      <view name=\"workload_management\" />\n      <view name=\"workload_management_per_pool_instance\" />\n      <view name=\"workload_management_per_pool_deployment\" />\n    <\/collection>\n  <\/collection>\n  <collection label=\"Forwarders\">\n    <view name=\"forwarder_instance\" />\n    <view name=\"forwarder_deployment\" />\n  <\/collection>\n  <collection label=\"Settings\">\n    <view name=\"monitoringconsole_configure\" />\n    <view name=\"monitoringconsole_forwarder_setup\" />\n    <view name=\"monitoringconsole_alerts_setup\" />\n    <view name=\"monitoringconsole_overview_preferences\"/>\n    <view name=\"monitoringconsole_check_list\" />\n  <\/collection>\n  <a href=\"search\">Run a Search<\/a>\n<\/nav>","eai:digest":"31adb23c29a2d1402e5867ee6b9b5a92","eai:userName":"nobody","rootNode":"nav"}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`
	splunkClientTester(t, "TestGetMonitoringconsoleAssetTable", 200, body, wantRequest, test)
}

func TestUpdateLookupUISettings(t *testing.T) {
	apiResponseUISettings := new(UISettings)
	apiResponseUISettings = &UISettings{
		Disabled:    false,
		EaiACL:      "",
		EaiAppName:  "splunk_monitoring_console",
		EaiUserName: "nobody",
	}
	wantconfiguredPeers := "&member=" + splcommon.TestUpdateLookupUISettings
	body := strings.NewReader("output_mode=json&trigger_actions=true&dispatch.auto_cancel=30&dispatch.buckets=300&dispatch.enablePreview=true")
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/configs/conf-splunk_monitoring_console_assets/settings", body)
	wantRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	test := func(c SplunkClient) error {
		return c.UpdateLookupUISettings(wantconfiguredPeers, apiResponseUISettings)
	}
	splunkClientTester(t, "TestPostMonitoringconsoleAssetTable", 200, "", wantRequest, test)
}

func TestUpdateMonitoringConsoleApp(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/system/apps/local/splunk_monitoring_console", nil)
	test := func(c SplunkClient) error {
		err := c.UpdateMonitoringConsoleApp()
		if err != nil {
			t.Errorf("MonitoringConsole App not updated")
		}
		return nil
	}
	splunkClientTester(t, "TestUpdateMonitoringConsoleApp", 200, "", wantRequest, test)
}

func TestGetClusterInfo(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/cluster/config?count=0&output_mode=json", nil)
	wantMultisite := ""
	test := func(c SplunkClient) error {
		info, err := c.GetClusterInfo(false)
		if err != nil {
			return err
		}
		if info.MultiSite != wantMultisite {
			t.Errorf("info.MultiSite=%s; want %s", info.MultiSite, wantMultisite)
		}
		return nil
	}
	body := splcommon.BodyTestGetClusterInfo
	splunkClientTester(t, "TestGetClusterInfo", 200, body, wantRequest, test)
}

func TestSetIdxcSecret(t *testing.T) {
	endpoint := fmt.Sprintf("https://localhost:8089/services/cluster/config/config?secret=%s", "changeme")
	wantRequest, _ := http.NewRequest("POST", endpoint, nil)
	wantRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	test := func(c SplunkClient) error {
		return c.SetIdxcSecret("changeme")
	}
	splunkClientTester(t, "TestSetIdxcSecret", 200, "", wantRequest, test)
}

func TestRestartSplunk(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/server/control/restart", nil)
	test := func(c SplunkClient) error {
		return c.RestartSplunk()
	}
	splunkClientTester(t, "TestRestartSplunk", 200, "", wantRequest, test)
}
