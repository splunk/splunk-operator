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

package client

import (
	"fmt"
	"net/http"
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
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/cluster/master/info?count=0&output_mode=json", nil)
	wantInfo := ClusterMasterInfo{
		Initialized:     true,
		IndexingReady:   true,
		ServiceReady:    true,
		MaintenanceMode: false,
		RollingRestart:  false,
		Label:           "splunk-s1-cluster-master-0",
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
	body := `{"links":{},"origin":"https://localhost:8089/services/cluster/master/info","updated":"2020-03-18T01:04:53+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"master","id":"https://localhost:8089/services/cluster/master/info/master","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/cluster/master/info/master","list":"/services/cluster/master/info/master"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"active_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/506c58d5aeda1dd6017889e3186e7337-1583870198.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","timestamp":1583870198},"apply_bundle_status":{"invalid_bundle":{"bundle_path":"","bundle_validation_errors_on_master":[],"checksum":"","timestamp":0},"reload_bundle_issued":false,"status":"None"},"backup_and_restore_primaries":false,"controlled_rolling_restart_flag":false,"eai:acl":null,"indexing_ready_flag":true,"initialized_flag":true,"label":"splunk-s1-cluster-master-0","last_check_restart_bundle_result":false,"last_dry_run_bundle":{"bundle_path":"","checksum":"","timestamp":0},"last_validated_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/0af7c0e95f313f7be3b0cb1d878df9a1-1583948640.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","is_valid_bundle":true,"timestamp":1583948640},"latest_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/506c58d5aeda1dd6017889e3186e7337-1583870198.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","timestamp":1583870198},"maintenance_mode":false,"multisite":false,"previous_active_bundle":{"bundle_path":"","checksum":"","timestamp":0},"primaries_backup_status":"No on-going (or) completed primaries backup yet. Check back again in few minutes if you expect a backup.","quiet_period_flag":false,"rolling_restart_flag":false,"rolling_restart_or_upgrade":false,"service_ready_flag":true,"start_time":1583948636,"summary_replication":"false"}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`
	splunkClientTester(t, "TestGetClusterMasterInfo", 200, body, wantRequest, test)

	// test body with no entries
	test = func(c SplunkClient) error {
		_, err := c.GetClusterMasterInfo()
		if err == nil {
			t.Errorf("GetClusterMasterInfo returned nil; want error")
		}
		return nil
	}
	body = `{"links":{},"origin":"https://localhost:8089/services/cluster/master/info","updated":"2020-03-18T01:04:53+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`
	splunkClientTester(t, "TestGetClusterMasterInfo", 200, body, wantRequest, test)

	// test error code
	splunkClientTester(t, "TestGetClusterMasterInfo", 500, "", wantRequest, test)
}

func TestGetIndexerClusterPeerInfo(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/cluster/slave/info?count=0&output_mode=json", nil)
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
	body := `{"links":{},"origin":"https://localhost:8089/services/cluster/slave/info","updated":"2020-03-18T01:28:18+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"slave","id":"https://localhost:8089/services/cluster/slave/info/slave","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/cluster/slave/info/slave","list":"/services/cluster/slave/info/slave"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"active_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/87c8c24e7fabc3ff9683c26652cb5890-1583870244.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","timestamp":1583870244},"base_generation_id":26,"eai:acl":null,"is_registered":true,"last_dry_run_bundle":{"bundle_path":"","checksum":"","timestamp":0},"last_heartbeat_attempt":0,"latest_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/87c8c24e7fabc3ff9683c26652cb5890-1583870244.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","timestamp":1583870244},"maintenance_mode":false,"registered_summary_state":3,"restart_state":"NoRestart","site":"default","status":"Up"}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`
	splunkClientTester(t, "TestGetIndexerClusterPeerInfo", 200, body, wantRequest, test)

	// test body with no entries
	test = func(c SplunkClient) error {
		_, err := c.GetIndexerClusterPeerInfo()
		if err == nil {
			t.Errorf("GetIndexerClusterPeerInfo returned nil; want error")
		}
		return nil
	}
	body = `{"links":{},"origin":"https://localhost:8089/services/cluster/slave/info","updated":"2020-03-18T01:28:18+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`
	splunkClientTester(t, "TestGetIndexerClusterPeerInfo", 200, body, wantRequest, test)

	// test error code
	splunkClientTester(t, "TestGetIndexerClusterPeerInfo", 500, "", wantRequest, test)
}

func TestGetClusterMasterPeers(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/cluster/master/peers?count=0&output_mode=json", nil)
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
	body := `{"links":{"create":"/services/cluster/master/peers/_new"},"origin":"https://localhost:8089/services/cluster/master/peers","updated":"2020-03-18T01:08:53+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"D39B1729-E2C5-4273-B9B2-534DA7C2F866","id":"https://localhost:8089/services/cluster/master/peers/D39B1729-E2C5-4273-B9B2-534DA7C2F866","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/cluster/master/peers/D39B1729-E2C5-4273-B9B2-534DA7C2F866","list":"/services/cluster/master/peers/D39B1729-E2C5-4273-B9B2-534DA7C2F866","edit":"/services/cluster/master/peers/D39B1729-E2C5-4273-B9B2-534DA7C2F866"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"active_bundle_id":"14310A4AABD23E85BBD4559C4A3B59F8","apply_bundle_status":{"invalid_bundle":{"bundle_validation_errors":[],"invalid_bundle_id":""},"reasons_for_restart":[],"restart_required_for_apply_bundle":false,"status":"None"},"base_generation_id":26,"bucket_count":73,"bucket_count_by_index":{"_audit":24,"_internal":45,"_telemetry":4},"buckets_rf_by_origin_site":{"default":73},"buckets_sf_by_origin_site":{"default":73},"delayed_buckets_to_discard":[],"eai:acl":null,"fixup_set":[],"heartbeat_started":true,"host_port_pair":"10.36.0.6:8089","indexing_disk_space":210707374080,"is_searchable":true,"is_valid_bundle":true,"label":"splunk-s1-indexer-0","last_dry_run_bundle":"","last_heartbeat":1584493732,"last_validated_bundle":"14310A4AABD23E85BBD4559C4A3B59F8","latest_bundle_id":"14310A4AABD23E85BBD4559C4A3B59F8","peer_registered_summaries":true,"pending_builds":[],"pending_job_count":0,"primary_count":73,"primary_count_remote":0,"register_search_address":"10.36.0.6:8089","replication_count":0,"replication_port":9887,"replication_use_ssl":false,"restart_required_for_applying_dry_run_bundle":false,"search_state_counter":{"PendingSearchable":0,"Searchable":73,"SearchablePendingMask":0,"Unsearchable":0},"site":"default","splunk_version":"8.0.2","status":"Up","status_counter":{"Complete":69,"NonStreamingTarget":0,"StreamingSource":4,"StreamingTarget":0},"summary_replication_count":0}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`
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
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/cluster/master/control/control/remove_peers?peers=D39B1729-E2C5-4273-B9B2-534DA7C2F866", nil)
	test := func(c SplunkClient) error {
		return c.RemoveIndexerClusterPeer("D39B1729-E2C5-4273-B9B2-534DA7C2F866")
	}
	splunkClientTester(t, "TestRemoveIndexerClusterPeer", 200, "", wantRequest, test)
}

func TestDecommissionIndexerClusterPeer(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/cluster/slave/control/control/decommission?enforce_counts=1", nil)
	test := func(c SplunkClient) error {
		return c.DecommissionIndexerClusterPeer(true)
	}
	splunkClientTester(t, "TestDecommissionIndexerClusterPeer", 200, "", wantRequest, test)
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
