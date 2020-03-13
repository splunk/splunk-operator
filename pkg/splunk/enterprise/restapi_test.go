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
	"io/ioutil"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// MockHTTPClient is used to replicate an http.Client
type MockHTTPClient struct {
	Requests []http.Request
	Response http.Response
	Err      error
}

// Do method for MockHTTPClient is called by all methods of SplunkClient
func (c *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.Requests = append(c.Requests, *req)
	return &c.Response, c.Err
}

func splunkClientTester(t *testing.T, testMethod string, status int, body string, wantRequests []http.Request, test func(SplunkClient) error) {
	mockClient := MockHTTPClient{
		Err: nil,
		Response: http.Response{
			StatusCode: status,
			Body:       ioutil.NopCloser(strings.NewReader(body)),
		},
	}
	c := NewSplunkClient("https://localhost:8089", "admin", "p@ssw0rd")
	c.client = &mockClient
	err := test(*c)
	if err != nil {
		t.Errorf("%s err = %v", testMethod, err)
	}
	if len(mockClient.Requests) != len(wantRequests) {
		t.Fatalf("%s got %d Requests; want %d", testMethod, len(mockClient.Requests), len(wantRequests))
	}
	for n := range mockClient.Requests {
		wantRequests[n].SetBasicAuth(c.username, c.password)
		if !reflect.DeepEqual(mockClient.Requests[n], wantRequests[n]) {
			t.Errorf("%s Requests[%d]=%v; want %v", testMethod, n, mockClient.Requests[n], wantRequests[n])
		}
	}
}

func TestGetSearchHeadCaptainInfo(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/shcluster/captain/info?count=0&output_mode=json", nil)
	want := []http.Request{*wantRequest}
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
	splunkClientTester(t, "TestGetSearchHeadCaptainInfo", 200, body, want, test)

	// test body with no entries
	test = func(c SplunkClient) error {
		_, err := c.GetSearchHeadCaptainInfo()
		if err == nil {
			t.Errorf("GetSearchHeadCaptainInfo returned nil; want error")
		}
		return nil
	}
	body = `{"links":{},"origin":"https://localhost:8089/services/shcluster/captain/info","updated":"2020-03-15T16:36:42+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[]}`
	splunkClientTester(t, "TestGetSearchHeadCaptainInfo", 200, body, want, test)

	// test empty body
	splunkClientTester(t, "TestGetSearchHeadCaptainInfo", 200, "", want, test)

	// test error code
	splunkClientTester(t, "TestGetSearchHeadCaptainInfo", 500, "", want, test)
}

func TestGetSearchHeadClusterMemberInfo(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/shcluster/member/info?count=0&output_mode=json", nil)
	want := []http.Request{*wantRequest}
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
	splunkClientTester(t, "TestGetSearchHeadClusterMemberInfo", 200, body, want, test)

	// test body with no entries
	test = func(c SplunkClient) error {
		_, err := c.GetSearchHeadClusterMemberInfo()
		if err == nil {
			t.Errorf("GetSearchHeadClusterMemberInfo returned nil; want error")
		}
		return nil
	}
	body = `{"links":{},"origin":"https://localhost:8089/services/shcluster/captain/info","updated":"2020-03-15T16:36:42+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[]}`
	splunkClientTester(t, "TestGetSearchHeadCaptainInfo", 200, body, want, test)

	// test empty body
	splunkClientTester(t, "TestGetSearchHeadClusterMemberInfo", 200, "", want, test)

	// test error code
	splunkClientTester(t, "TestGetSearchHeadClusterMemberInfo", 500, "", want, test)
}

func TestGetSearchHeadCaptainMembers(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/shcluster/captain/members?count=0&output_mode=json", nil)
	want := []http.Request{*wantRequest}
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
	splunkClientTester(t, "TestGetSearchHeadCaptainMembers", 200, body, want, test)

	// test error response
	test = func(c SplunkClient) error {
		_, err := c.GetSearchHeadCaptainMembers()
		if err == nil {
			t.Errorf("GetSearchHeadCaptainMembers returned nil; want error")
		}
		return nil
	}
	splunkClientTester(t, "TestGetSearchHeadCaptainMembers", 503, "", want, test)
}

func TestSetSearchHeadDetention(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/shcluster/member/control/control/set_manual_detention?manual_detention=on", nil)
	want := []http.Request{*wantRequest}
	test := func(c SplunkClient) error {
		return c.SetSearchHeadDetention(true)
	}
	splunkClientTester(t, "TestSetSearchHeadDetention", 200, "", want, test)
}

func TestRemoveSearchHeadClusterMember(t *testing.T) {
	// test for 200 response first (sent on first removal request)
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/shcluster/member/consensus/default/remove_server?output_mode=json", nil)
	want := []http.Request{*wantRequest}
	test := func(c SplunkClient) error {
		return c.RemoveSearchHeadClusterMember()
	}
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 200, "", want, test)

	// next test 503 error message (sent for short period after removal, while SH is updating itself)
	body := `{"messages":[{"type":"ERROR","text":"Failed to proxy call to member https://splunk-s2-search-head-0.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089. ERROR:  Server https://splunk-s2-search-head-3.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089 is not part of configuration, hence cannot be removed. Check configuration by making GET request onto /services/shcluster/member/consensus"}]}`
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 503, body, want, test)

	// check alternate 503 message (sent after SH has completed removal)
	body = `{"messages":[{"type":"ERROR","text":"This node is not part of any cluster configuration, please re-run the command from an active cluster member. Also see \"splunk add shcluster-member\" to add this member to an existing cluster or see \"splunk bootstrap shcluster-captain\" to bootstrap a new cluster with this member."}]}`
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 503, body, want, test)

	// test unrecognized response message
	test = func(c SplunkClient) error {
		err := c.RemoveSearchHeadClusterMember()
		if err == nil {
			t.Errorf("RemoveSearchHeadClusterMember returned nil; want error")
		}
		return nil
	}
	body = `{"messages":[{"type":"ERROR","text":"Nothing that we are expecting."}]}`
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 503, body, want, test)

	// test empty messages array in response
	body = `{"messages":[]}`
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 503, body, want, test)

	// test unmarshal failure
	body = `<invalid>`
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 503, body, want, test)

	// test empty response
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 503, "", want, test)

	// test bad response code
	splunkClientTester(t, "TestRemoveSearchHeadClusterMember", 404, "", want, test)
}
