// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package splunk_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	splunk "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

var invalidUrlByteArray = []byte{0x7F}

// Error tester for client
func splunkClientErrorTester(t *testing.T, test func(splunk.SplunkClient) error) {
	url := string(invalidUrlByteArray)
	mockSplunkClient := &spltest.MockHTTPClient{}
	c := splunk.NewSplunkClient(url, "admin", "p@ssw0rd")
	c.Client = mockSplunkClient
	c.SearchHeadClusterUpgradeClient = mockSplunkClient
	err := test(*c)
	if err == nil {
		t.Errorf("Expected error, err = %v", err)
	}
}

func splunkClientTester(t *testing.T, testMethod string, status int, body string, wantRequest *http.Request, test func(splunk.SplunkClient) error) {
	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandler(wantRequest, status, body, nil)
	c := splunk.NewSplunkClient("https://localhost:8089", "admin", "p@ssw0rd")
	c.Client = mockSplunkClient
	c.SearchHeadClusterUpgradeClient = mockSplunkClient
	err := test(*c)
	if err != nil {
		t.Errorf("%s err = %v", testMethod, err)
	}
	mockSplunkClient.CheckRequests(t, testMethod)
}

func splunkClientMultipleRequestTester(t *testing.T, testMethod string, status []int, body []string, wantRequest []*http.Request, test func(splunk.SplunkClient) error) {
	mockSplunkClient := &spltest.MockHTTPClient{}
	for i := 0; i < len(wantRequest); i++ {
		mockSplunkClient.AddHandler(wantRequest[i], status[i], body[i], nil)
	}
	c := splunk.NewSplunkClient("https://localhost:8089", "admin", "p@ssw0rd")
	c.Client = mockSplunkClient
	c.SearchHeadClusterUpgradeClient = mockSplunkClient
	err := test(*c)
	if err != nil {
		t.Errorf("%s err = %v", testMethod, err)
	}
	mockSplunkClient.CheckRequests(t, testMethod)
}

func TestSplunkClientDo(t *testing.T) {
	// Test error in do
	mockSplunkClient := &spltest.MockHTTPClient{}
	c := splunk.NewSplunkClient("https://localhost:8089", "admin", "p@ssw0rd")
	c.Client = mockSplunkClient
	hreq := http.Request{
		Header: http.Header{},
		URL:    &url.URL{},
		Method: "abcd",
	}
	c.Do(&hreq, []int{200}, nil)
}

func TestNewSplunkClientRequestTimeouts(t *testing.T) {
	c := splunk.NewSplunkClient("https://localhost:8089", "admin", "p@ssw0rd")

	defaultClient, ok := c.Client.(*http.Client)
	if !ok {
		t.Fatalf("Client type=%T; want *http.Client", c.Client)
	}
	if defaultClient.Timeout != 5*time.Second {
		t.Errorf("Client.Timeout=%s; want %s", defaultClient.Timeout, 5*time.Second)
	}

	upgradeClient, ok := c.SearchHeadClusterUpgradeClient.(*http.Client)
	if !ok {
		t.Fatalf("SearchHeadClusterUpgradeClient type=%T; want *http.Client", c.SearchHeadClusterUpgradeClient)
	}
	if upgradeClient.Timeout != 60*time.Second {
		t.Errorf("SearchHeadClusterUpgradeClient.Timeout=%s; want %s", upgradeClient.Timeout, 60*time.Second)
	}
}

func TestSearchHeadClusterUpgradeUsesSpecializedClient(t *testing.T) {
	upgradeClient := &spltest.MockHTTPClient{}
	initRequest, _ := http.NewRequest(
		"POST",
		"https://localhost:8089/services/shcluster/captain/control/control/upgrade-init",
		nil,
	)
	finalizeRequest, _ := http.NewRequest(
		"POST",
		"https://localhost:8089/services/shcluster/captain/control/control/upgrade-finalize",
		nil,
	)
	upgradeClient.AddHandler(initRequest, http.StatusOK, "", nil)
	upgradeClient.AddHandler(finalizeRequest, http.StatusOK, "", nil)

	defaultClient := &spltest.MockHTTPClient{}
	c := splunk.NewSplunkClient("https://localhost:8089", "admin", "p@ssw0rd")
	c.Client = defaultClient
	c.SearchHeadClusterUpgradeClient = upgradeClient

	if err := c.InitiateUpgrade(); err != nil {
		t.Fatalf("InitiateUpgrade() error=%v", err)
	}
	if err := c.FinalizeUpgrade(); err != nil {
		t.Fatalf("FinalizeUpgrade() error=%v", err)
	}

	upgradeClient.CheckRequests(t, "TestSearchHeadClusterUpgradeUsesSpecializedClient")
	if len(defaultClient.GotRequests) != 0 {
		t.Fatalf("default client received %d requests; want 0", len(defaultClient.GotRequests))
	}
}

func TestGetSearchHeadCaptainInfo(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/shcluster/captain/info?count=0&output_mode=json", nil)
	wantCaptainLabel := "splunk-s2-search-head-0"
	test := func(c splunk.SplunkClient) error {
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
	test = func(c splunk.SplunkClient) error {
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
	test := func(c splunk.SplunkClient) error {
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
	test = func(c splunk.SplunkClient) error {
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
	test := func(c splunk.SplunkClient) error {
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
			if member.Identifier == "" {
				t.Errorf("member %s has empty persistent identifier", wantMembers[n])
			}
			wantManagementURI := fmt.Sprintf("https://%s.splunk-s2-search-head-headless.splunk.svc.cluster.local:8089", wantMembers[n])
			if member.ManagementURI != wantManagementURI {
				t.Errorf("member %s want ManagementURI=%s: got %s", wantMembers[n], wantManagementURI, member.ManagementURI)
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
	test = func(c splunk.SplunkClient) error {
		_, err := c.GetSearchHeadCaptainMembers()
		if err == nil {
			t.Errorf("GetSearchHeadCaptainMembers returned nil; want error")
		}
		return nil
	}
	splunkClientTester(t, "TestGetSearchHeadCaptainMembers", 503, "", wantRequest, test)
}

func TestGetKVStoreStatus(t *testing.T) {
	wantRequest, _ := http.NewRequest(
		"GET",
		"https://localhost:8089/services/kvstore/status?count=0&output_mode=json",
		nil,
	)
	test := func(c splunk.SplunkClient) error {
		status, err := c.GetKVStoreStatus()
		if err != nil {
			return err
		}
		if status.Current.Status != "ready" {
			t.Errorf("KV Store status=%q; want ready", status.Current.Status)
		}
		return nil
	}
	body := `{"entry":[{"content":{"current":{"status":"ready"}}}]}`
	splunkClientTester(t, "TestGetKVStoreStatus", 200, body, wantRequest, test)

	test = func(c splunk.SplunkClient) error {
		_, err := c.GetKVStoreStatus()
		if err == nil {
			t.Error("GetKVStoreStatus returned nil; want invalid response error")
		}
		return nil
	}
	splunkClientTester(t, "TestGetKVStoreStatus", 200, `{"entry":[]}`, wantRequest, test)
	splunkClientTester(t, "TestGetKVStoreStatus", 200, `{"entry":[{"content":{"current":{}}}]}`, wantRequest, test)
	splunkClientTester(t, "TestGetKVStoreStatus", 503, "", wantRequest, test)
	splunkClientErrorTester(t, func(c splunk.SplunkClient) error {
		_, err := c.GetKVStoreStatus()
		return err
	})
}

func TestSetSearchHeadDetention(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/shcluster/member/control/control/set_manual_detention?manual_detention=on", nil)
	test := func(c splunk.SplunkClient) error {
		return c.SetSearchHeadDetention(true)
	}
	splunkClientTester(t, "TestSetSearchHeadDetention", 200, "", wantRequest, test)

	// Negative testing
	splunkClientErrorTester(t, test)
}

func TestTransferSearchHeadCaptain(t *testing.T) {
	body := strings.NewReader("mgmt_uri=https%3A%2F%2Fsplunk-example-search-head-1%3A8089")
	wantRequest, _ := http.NewRequest(
		"POST",
		"https://localhost:8089/services/shcluster/member/consensus/default/transfer_captaincy",
		body,
	)
	wantRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	test := func(c splunk.SplunkClient) error {
		return c.TransferSearchHeadCaptain("https://splunk-example-search-head-1:8089")
	}
	splunkClientTester(t, "TestTransferSearchHeadCaptain", 200, "", wantRequest, test)

	splunkClientErrorTester(t, test)
}

func TestInitiateSearchHeadRollingRestart(t *testing.T) {
	body := strings.NewReader("advertising=true")
	wantRequest, _ := http.NewRequest(
		"POST",
		"https://localhost:8089/services/shcluster/captain/control/control/restart",
		body,
	)
	wantRequest.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)
	test := func(c splunk.SplunkClient) error {
		c.SearchHeadClusterUpgradeClient = c.Client
		return c.InitiateSearchHeadRollingRestart(true)
	}
	splunkClientTester(
		t,
		"TestInitiateSearchHeadRollingRestart",
		200,
		"",
		wantRequest,
		test,
	)

	splunkClientErrorTester(t, test)
}

func TestBundlePush(t *testing.T) {
	body := strings.NewReader("&ignore_identical_bundle=true")
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/cluster/manager/control/default/apply", body)

	test := func(c splunk.SplunkClient) error {
		return c.BundlePush(true)
	}
	splunkClientTester(t, "TestBundlePush", 200, "", wantRequest, test)

	// Negative testing
	splunkClientErrorTester(t, test)
}

func TestRemoveSearchHeadClusterMember(t *testing.T) {
	// test for 200 response first (sent on first removal request)
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/shcluster/member/consensus/default/remove_server?output_mode=json", nil)
	test := func(c splunk.SplunkClient) error {
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
	test = func(c splunk.SplunkClient) error {
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

	// Negative testing
	test = func(c splunk.SplunkClient) error {
		return c.RemoveSearchHeadClusterMember()
	}
	splunkClientErrorTester(t, test)
}

func TestGetclusterManagerInfo(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/cluster/manager/info?count=0&output_mode=json", nil)
	wantInfo := splunk.ClusterManagerInfo{
		Initialized:     true,
		IndexingReady:   true,
		ServiceReady:    true,
		MaintenanceMode: false,
		RollingRestart:  false,
		Label:           fmt.Sprintf("splunk-%s-cluster-manager-%s", "s1", "0"),
		ActiveBundle: splunk.ClusterBundleInfo{
			BundlePath: "/opt/splunk/var/run/splunk/cluster/remote-bundle/506c58d5aeda1dd6017889e3186e7337-1583870198.bundle",
			Checksum:   "14310A4AABD23E85BBD4559C4A3B59F8",
			Timestamp:  1583870198,
		},
		LatestBundle: splunk.ClusterBundleInfo{
			BundlePath: "/opt/splunk/var/run/splunk/cluster/remote-bundle/506c58d5aeda1dd6017889e3186e7337-1583870198.bundle",
			Checksum:   "14310A4AABD23E85BBD4559C4A3B59F8",
			Timestamp:  1583870198,
		},
		StartTime: 1583948636,
	}
	test := func(c splunk.SplunkClient) error {
		gotInfo, err := c.GetClusterManagerInfo()
		if err != nil {
			return err
		}
		if *gotInfo != wantInfo {
			t.Errorf("info.Status=%v; want %v", *gotInfo, wantInfo)
		}
		return nil
	}
	body := `{"links":{},"origin":"https://localhost:8089/services/cluster/manager/info","updated":"2020-03-18T01:04:53+00:00","generator":{"build":"a7f645ddaf91","version":"8.0.2"},"entry":[{"name":"manager","id":"https://localhost:8089/services/cluster/manager/info/master","updated":"1970-01-01T00:00:00+00:00","links":{"alternate":"/services/cluster/manager/info/master","list":"/services/cluster/manager/info/master"},"author":"system","acl":{"app":"","can_list":true,"can_write":true,"modifiable":false,"owner":"system","perms":{"read":["admin","splunk-system-role"],"write":["admin","splunk-system-role"]},"removable":false,"sharing":"system"},"content":{"active_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/506c58d5aeda1dd6017889e3186e7337-1583870198.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","timestamp":1583870198},"apply_bundle_status":{"invalid_bundle":{"bundle_path":"","bundle_validation_errors_on_master":[],"checksum":"","timestamp":0},"reload_bundle_issued":false,"status":"None"},"backup_and_restore_primaries":false,"controlled_rolling_restart_flag":false,"eai:acl":null,"indexing_ready_flag":true,"initialized_flag":true,"label":"splunk-s1-cluster-manager-0","last_check_restart_bundle_result":false,"last_dry_run_bundle":{"bundle_path":"","checksum":"","timestamp":0},"last_validated_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/0af7c0e95f313f7be3b0cb1d878df9a1-1583948640.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","is_valid_bundle":true,"timestamp":1583948640},"latest_bundle":{"bundle_path":"/opt/splunk/var/run/splunk/cluster/remote-bundle/506c58d5aeda1dd6017889e3186e7337-1583870198.bundle","checksum":"14310A4AABD23E85BBD4559C4A3B59F8","timestamp":1583870198},"maintenance_mode":false,"multisite":false,"previous_active_bundle":{"bundle_path":"","checksum":"","timestamp":0},"primaries_backup_status":"No on-going (or) completed primaries backup yet. Check back again in few minutes if you expect a backup.","quiet_period_flag":false,"rolling_restart_flag":false,"rolling_restart_or_upgrade":false,"service_ready_flag":true,"start_time":1583948636,"summary_replication":"false"}}],"paging":{"total":1,"perPage":30,"offset":0},"messages":[]}`
	splunkClientTester(t, "TestGetclusterManagerInfo", 200, body, wantRequest, test)

	// test body with no entries
	test = func(c splunk.SplunkClient) error {
		_, err := c.GetClusterManagerInfo()
		if err == nil {
			t.Errorf("GetClusterManagerInfo returned nil; want error")
		}
		return nil
	}
	body = loadFixture(t, "get_cm_info_empty.json")
	splunkClientTester(t, "TestGetclusterManagerInfo", 200, body, wantRequest, test)

	// test error code
	splunkClientTester(t, "TestGetClusterManagerInfo", 500, "", wantRequest, test)
}

func TestGetIndexerClusterPeerInfo(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/cluster/peer/info?count=0&output_mode=json", nil)
	wantMemberStatus := "Up"
	test := func(c splunk.SplunkClient) error {
		info, err := c.GetIndexerClusterPeerInfo()
		if err != nil {
			return err
		}
		if info.Status != wantMemberStatus {
			t.Errorf("info.Status=%s; want %s", info.Status, wantMemberStatus)
		}
		return nil
	}
	body := loadFixture(t, "get_indexer_cluster_peer_info.json")
	splunkClientTester(t, "TestGetIndexerClusterPeerInfo", 200, body, wantRequest, test)

	// test body with no entries
	test = func(c splunk.SplunkClient) error {
		_, err := c.GetIndexerClusterPeerInfo()
		if err == nil {
			t.Errorf("GetIndexerClusterPeerInfo returned nil; want error")
		}
		return nil
	}
	body = loadFixture(t, "get_indexer_cluster_peer_info_empty.json")
	splunkClientTester(t, "TestGetIndexerClusterPeerInfo", 200, body, wantRequest, test)

	// test error code
	splunkClientTester(t, "TestGetIndexerClusterPeerInfo", 500, "", wantRequest, test)
}

func TestGetClusterManagerPeers(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/cluster/manager/peers?count=0&output_mode=json", nil)
	var wantPeers = []struct {
		ID     string
		Label  string
		Status string
	}{
		{ID: "D39B1729-E2C5-4273-B9B2-534DA7C2F866", Label: "splunk-s1-indexer-0", Status: "Up"},
	}
	test := func(c splunk.SplunkClient) error {
		peers, err := c.GetClusterManagerPeers()
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
	body := loadFixture(t, "get_cm_peers.json")
	splunkClientTester(t, "TestGetClusterManagerPeers", 200, body, wantRequest, test)

	// test error response
	test = func(c splunk.SplunkClient) error {
		_, err := c.GetClusterManagerPeers()
		if err == nil {
			t.Errorf("GetClusterManagerPeers returned nil; want error")
		}
		return nil
	}
	splunkClientTester(t, "TestGetClusterManagerPeers", 503, "", wantRequest, test)
}

func TestRemoveIndexerClusterPeer(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/cluster/manager/control/control/remove_peers?peers=D39B1729-E2C5-4273-B9B2-534DA7C2F866", nil)
	test := func(c splunk.SplunkClient) error {
		return c.RemoveIndexerClusterPeer("D39B1729-E2C5-4273-B9B2-534DA7C2F866")
	}
	splunkClientTester(t, "TestRemoveIndexerClusterPeer", 200, "", wantRequest, test)

	// Negative testing
	splunkClientErrorTester(t, test)
}

func TestDecommissionIndexerClusterPeer(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/cluster/peer/control/control/decommission?enforce_counts=1", nil)
	test := func(c splunk.SplunkClient) error {
		return c.DecommissionIndexerClusterPeer(true)
	}
	splunkClientTester(t, "TestDecommissionIndexerClusterPeer", 200, "", wantRequest, test)

	// Negative testing
	splunkClientErrorTester(t, test)
}

func TestAutomateMCApplyChanges(t *testing.T) {
	request1, _ := http.NewRequest("GET", "https://localhost:8089/services/server/info/server-info?count=0&output_mode=json", nil)
	request2, _ := http.NewRequest("GET", "https://localhost:8089/services/search/distributed/peers?count=0&output_mode=json", nil)
	request3, _ := http.NewRequest("POST", "https://localhost:8089/services/search/distributed/groups/dmc_group_indexer/edit", nil)
	request4, _ := http.NewRequest("POST", splcommon.LocalURLLicenseManagerEdit, nil)
	request5, _ := http.NewRequest("POST", "https://localhost:8089/services/search/distributed/groups/dmc_indexerclustergroup_idxc_label/edit", nil)
	request6, _ := http.NewRequest("GET", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/saved/searches/DMC%20Asset%20-%20Build%20Full?count=0&output_mode=json", nil)
	request7, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/saved/searches/DMC%20Asset%20-%20Build%20Full/dispatch", nil)
	request8, _ := http.NewRequest("GET", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed?count=0&output_mode=json", nil)
	request9, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/configs/conf-splunk_monitoring_console_assets/settings", nil)
	request10, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/system/apps/local/splunk_monitoring_console", nil)
	var wantRequests []*http.Request
	wantRequests = []*http.Request(append(wantRequests, request1, request2, request3, request4, request5, request6, request7, request8, request9, request10))
	body := []string{
		loadFixture(t, "get_server_info.json"),
		loadFixture(t, "mc_apply_changes.json"),
		"",
		"",
		"",
		loadFixture(t, "get_mc_nav_default_distributed.json"),
		"",
		loadFixture(t, "get_mc_nav_default_distributed.json"),
		"",
		"",
	}
	test := func(c splunk.SplunkClient) error {
		return c.AutomateMCApplyChanges()
	}
	status := []int{
		200, 200, 200, 200, 200, 200, 201, 200, 200, 200, 200,
	}
	splunkClientMultipleRequestTester(t, "TestAutomateMCApplyChanges", status, body, wantRequests, test)
}

func TestGetSearchDistributedPeers(t *testing.T) {
	wantRequest, _ := http.NewRequest(
		"GET",
		"https://localhost:8089/services/search/distributed/peers?count=0&output_mode=json",
		nil,
	)
	body := `{"entry":[{"name":"10.0.1.4:8089","content":{"guid":"peer-guid","status":"Up","disabled":false}},{"name":"10.0.0.4:8089","content":{"guid":"peer-guid","status":"Down","disabled":false}}]}`
	test := func(c splunk.SplunkClient) error {
		peers, err := c.GetSearchDistributedPeers()
		if err != nil {
			return err
		}
		wantPeers := []splunk.SearchDistributedPeerInfo{
			{Name: "10.0.1.4:8089", ID: "peer-guid", Status: "Up", Disabled: false},
			{Name: "10.0.0.4:8089", ID: "peer-guid", Status: "Down", Disabled: false},
		}
		if !reflect.DeepEqual(wantPeers, peers) {
			t.Errorf("GetSearchDistributedPeers() = %#v, want %#v", peers, wantPeers)
		}
		return nil
	}
	splunkClientTester(t, "TestGetSearchDistributedPeers", 200, body, wantRequest, test)

	// Negative testing
	splunkClientErrorTester(t, test)
}
func TestGetMonitoringconsoleServerRoles(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/server/info/server-info?count=0&output_mode=json", nil)
	test := func(c splunk.SplunkClient) error {
		info, err := c.GetMonitoringconsoleServerRoles()
		if err != nil {
			return err
		}
		if len(info.ServerRoles) == 0 {
			t.Errorf("There should be atleast one server role assigned to this host")
		}
		return nil
	}
	body := loadFixture(t, "get_cluster_info.json")
	splunkClientTester(t, "TestGetMonitoringconsoleServerRoles", 200, body, wantRequest, test)

	// Test negative conditions
	url := string(invalidUrlByteArray)
	mockSplunkHttpClient := &spltest.MockHTTPClient{}
	c := splunk.NewSplunkClient(url, "admin", "p@ssw0rd")
	c.Client = mockSplunkHttpClient
	c.GetMonitoringconsoleServerRoles()
}
func TestUpdateDMCGroups(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/search/distributed/groups/indexer/edit", nil)
	test := func(c splunk.SplunkClient) error {
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
	test := func(c splunk.SplunkClient) error {
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
	test := func(c splunk.SplunkClient) error {
		info, err := c.GetMonitoringconsoleAssetTable()
		if err != nil {
			return err
		}
		if info.DispatchBuckets != wantDispatchBuckets {
			t.Errorf("info.Status=%d; want %d", info.DispatchBuckets, wantDispatchBuckets)
		}
		return nil
	}
	body := loadFixture(t, "get_mc_asset_table.json")
	splunkClientTester(t, "TestGetMonitoringconsoleAssetTable", 200, body, wantRequest, test)

	// Test negative conditions
	url := string(invalidUrlByteArray)
	mockSplunkHttpClient := &spltest.MockHTTPClient{}
	c := splunk.NewSplunkClient(url, "admin", "p@ssw0rd")
	c.Client = mockSplunkHttpClient
	c.GetMonitoringconsoleAssetTable()
}

func TestPostMonitoringConsoleAssetTable(t *testing.T) {
	apiResponseMCAssetBuild := new(splunk.MCAssetBuildTable)
	apiResponseMCAssetBuild = &splunk.MCAssetBuildTable{
		DispatchAutoCancel: "30",
		DispatchBuckets:    int64(0),
	}

	body := strings.NewReader("output_mode=json&trigger_actions=true&dispatch.auto_cancel=30&dispatch.buckets=300&dispatch.enablePreview=true")
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/saved/searches/DMC%20Asset%20-%20Build%20Full/dispatch", body)
	wantRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	test := func(c splunk.SplunkClient) error {
		return c.PostMonitoringConsoleAssetTable(apiResponseMCAssetBuild)
	}
	splunkClientTester(t, "TestPostMonitoringConsoleAssetTable", 201, "", wantRequest, test)
}

func TestGetMonitoringConsoleUISettings(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/data/ui/nav/default.distributed?count=0&output_mode=json", nil)
	wantEaiAppName := "splunk_monitoring_console"
	test := func(c splunk.SplunkClient) error {
		info, err := c.GetMonitoringConsoleUISettings()
		if err != nil {
			return err
		}
		if info.EaiAppName != wantEaiAppName {
			t.Errorf("info.Status=%s; want %s", info.EaiAppName, wantEaiAppName)
		}
		return nil
	}
	body := loadFixture(t, "get_mc_nav_default_distributed.json")
	splunkClientTester(t, "TestGetMonitoringconsoleAssetTable", 200, body, wantRequest, test)

	// Test negative conditions
	url := string(invalidUrlByteArray)
	mockSplunkHttpClient := &spltest.MockHTTPClient{}
	c := splunk.NewSplunkClient(url, "admin", "p@ssw0rd")
	c.Client = mockSplunkHttpClient
	c.GetMonitoringConsoleUISettings()
}

func TestUpdateLookupUISettings(t *testing.T) {
	apiResponseUISettings := new(splunk.UISettings)
	apiResponseUISettings = &splunk.UISettings{
		Disabled:    false,
		EaiACL:      "",
		EaiAppName:  "splunk_monitoring_console",
		EaiUserName: "nobody",
	}
	wantconfiguredPeers := "&member=splunk-example-cluster-manager-service:8089&"
	body := strings.NewReader("output_mode=json&trigger_actions=true&dispatch.auto_cancel=30&dispatch.buckets=300&dispatch.enablePreview=true")
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/splunk_monitoring_console/configs/conf-splunk_monitoring_console_assets/settings", body)
	wantRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	test := func(c splunk.SplunkClient) error {
		return c.UpdateLookupUISettings(wantconfiguredPeers, apiResponseUISettings)
	}
	splunkClientTester(t, "TestPostMonitoringconsoleAssetTable", 200, "", wantRequest, test)

	// Test negative conditions
	url := string(invalidUrlByteArray)
	mockSplunkHttpClient := &spltest.MockHTTPClient{}
	c := splunk.NewSplunkClient(url, "admin", "p@ssw0rd")
	c.Client = mockSplunkHttpClient
	c.GetMonitoringConsoleUISettings()
}

func TestUpdateMonitoringConsoleApp(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/system/apps/local/splunk_monitoring_console", nil)
	test := func(c splunk.SplunkClient) error {
		err := c.UpdateMonitoringConsoleApp()
		if err != nil {
			t.Log("MonitoringConsole App not updated")
			return err
		}
		return nil
	}
	splunkClientTester(t, "TestUpdateMonitoringConsoleApp", 200, "", wantRequest, test)

	// Test invalid http request
	splunkClientErrorTester(t, test)
}

func TestGetClusterInfo(t *testing.T) {
	wantRequest, _ := http.NewRequest("GET", "https://localhost:8089/services/cluster/config?count=0&output_mode=json", nil)
	wantMultisite := ""
	test := func(c splunk.SplunkClient) error {
		info, err := c.GetClusterInfo(false)
		if err != nil {
			return err
		}
		if info.MultiSite != wantMultisite {
			t.Errorf("info.MultiSite=%s; want %s", info.MultiSite, wantMultisite)
		}
		return nil
	}
	body := loadFixture(t, "get_cluster_info.json")
	splunkClientTester(t, "TestGetClusterInfo", 200, body, wantRequest, test)

	// Test negative conditions
	url := string(invalidUrlByteArray)

	// Test mock call
	mockSplunkHttpClient := &spltest.MockHTTPClient{}
	c := splunk.NewSplunkClient(url, "admin", "p@ssw0rd")
	c.Client = mockSplunkHttpClient
	c.GetClusterInfo(true)

	// Test get call error
	c.GetClusterInfo(false)
}

func TestSetIdxcSecret(t *testing.T) {
	endpoint := fmt.Sprintf("https://localhost:8089/services/cluster/config/config?secret=%s", "changeme")
	wantRequest, _ := http.NewRequest("POST", endpoint, nil)
	wantRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	test := func(c splunk.SplunkClient) error {
		return c.SetIdxcSecret("changeme")
	}
	splunkClientTester(t, "TestSetIdxcSecret", 200, "", wantRequest, test)

	// Test invalid http request
	splunkClientErrorTester(t, test)
}

func TestSendTelemetry_Success(t *testing.T) {
	path := "/services/telemetry/metrics"
	bodyBytes := []byte(`{"metric":"value"}`)
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/telemetry/metrics", bytes.NewReader(bodyBytes))
	wantRequest.Header.Set("Content-Type", "application/json")
	wantResponse := splunk.TelemetryResponse{
		Message:       "Telemetry sent successfully",
		MetricValueID: "abc123",
	}
	test := func(c splunk.SplunkClient) error {
		resp, err := c.SendTelemetry(path, bodyBytes)
		if err != nil {
			return err
		}
		if resp.Message != wantResponse.Message || resp.MetricValueID != wantResponse.MetricValueID {
			t.Errorf("SendTelemetry = %+v; want %+v", resp, wantResponse)
		}
		return nil
	}
	responseBody := `{"message":"Telemetry sent successfully","metricValueId":"abc123"}`
	splunkClientTester(t, "TestSendTelemetry", 201, responseBody, wantRequest, test)
}

func TestSendTelemetry_Error(t *testing.T) {
	path := "/services/telemetry/metrics"
	bodyBytes := []byte(`{"metric":"value"}`)
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/telemetry/metrics", bytes.NewReader(bodyBytes))
	wantRequest.Header.Set("Content-Type", "application/json")

	test := func(c splunk.SplunkClient) error {
		_, err := c.SendTelemetry(path, bodyBytes)
		if err == nil {
			t.Errorf("SendTelemetry should return error for 500 response code")
		}
		return nil
	}

	// Simulate a 500 error response from the mock client
	splunkClientTester(t, "TestSendTelemetry_Error", 500, "", wantRequest, test)
}

func TestRestartSplunk(t *testing.T) {
	wantRequest, _ := http.NewRequest("POST", "https://localhost:8089/services/server/control/restart", nil)
	test := func(c splunk.SplunkClient) error {
		return c.RestartSplunk()
	}
	splunkClientTester(t, "TestRestartSplunk", 200, "", wantRequest, test)

	// Test invalid http request
	splunkClientErrorTester(t, test)
}

func TestUpdateConfFile(t *testing.T) {
	// Test successful creation and update of conf property
	property := "myproperty"
	key := "mykey"
	value := "myvalue"
	fileName := "outputs"

	ctx := context.TODO()

	// First request: create the property (object) if it doesn't exist
	createBody := strings.NewReader(fmt.Sprintf("name=%s", property))
	wantCreateRequest, _ := http.NewRequest("POST", "https://localhost:8089/servicesNS/nobody/system/configs/conf-outputs", createBody)

	// Second request: update the key/value for the property
	updateBody := strings.NewReader(fmt.Sprintf("%s=%s", key, value))
	wantUpdateRequest, _ := http.NewRequest("POST", fmt.Sprintf("https://localhost:8089/servicesNS/nobody/system/configs/conf-outputs/%s", property), updateBody)

	mockSplunkClient := &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandler(wantCreateRequest, 201, "", nil)
	mockSplunkClient.AddHandler(wantUpdateRequest, 200, "", nil)

	c := splunk.NewSplunkClient("https://localhost:8089", "admin", "p@ssw0rd")
	c.Client = mockSplunkClient

	err := c.UpdateConfFile(ctx, fileName, property, [][]string{{key, value}})
	if err != nil {
		t.Errorf("UpdateConfFile err = %v", err)
	}
	mockSplunkClient.CheckRequests(t, "TestUpdateConfFile")

	// Negative test: error on create
	mockSplunkClient = &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandler(wantCreateRequest, 500, "", nil)
	c.Client = mockSplunkClient
	err = c.UpdateConfFile(ctx, fileName, property, [][]string{{key, value}})
	if err == nil {
		t.Errorf("UpdateConfFile expected error on create, got nil")
	}

	// Negative test: error on update
	mockSplunkClient = &spltest.MockHTTPClient{}
	mockSplunkClient.AddHandler(wantCreateRequest, 201, "", nil)
	mockSplunkClient.AddHandler(wantUpdateRequest, 500, "", nil)
	c.Client = mockSplunkClient
	err = c.UpdateConfFile(ctx, fileName, property, [][]string{{key, value}})
	if err == nil {
		t.Errorf("UpdateConfFile expected error on update, got nil")
	}
}
