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

package shc

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func newCaptainTransferCheckpointManager(cr *enterpriseApi.SearchHeadCluster, httpClient *spltest.MockHTTPClient) *PodManager {
	c := spltest.NewMockClient()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "checkpoint-secret", Namespace: cr.Namespace},
		Data:       map[string][]byte{"password": []byte("test-password")},
	}
	c.AddObject(secret)
	for _, member := range cr.Status.Members {
		c.AddObject(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: member.Name, Namespace: cr.Namespace},
			Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
				Name: "mnt-splunk-secrets",
				VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
					SecretName: secret.Name,
				}},
			}}},
		})
	}
	return &PodManager{
		Client: c,
		CR:     cr,
		NewSplunkClient: func(managementURI, username, password string) *splclient.SplunkClient {
			c := splclient.NewSplunkClient(managementURI, username, password)
			c.Client = httpClient
			return c
		},
	}
}

func assertCaptainTransferRequests(t *testing.T, httpClient *spltest.MockHTTPClient, targetURI string) {
	t.Helper()
	httpClient.CheckRequests(t, t.Name())
	for _, req := range httpClient.GotRequests {
		if req.Method != http.MethodPost {
			t.Errorf("request method = %q; want POST", req.Method)
		}
		if err := req.ParseForm(); err != nil {
			t.Fatalf("parse request form: %v", err)
		}
	}
	if got := httpClient.GotRequests[0].Form.Get("preferred_captain"); got != "false" {
		t.Errorf("preferred_captain = %q; want false", got)
	}
	if got := httpClient.GotRequests[1].Form.Get("mgmt_uri"); got != targetURI {
		t.Errorf("transfer target = %q; want %q", got, targetURI)
	}
}

func TestPrepareRecycleCaptainTransferCheckpoint(t *testing.T) {
	for _, tc := range []struct {
		name             string
		status           string
		activeSearches   int
		initialOperation enterpriseApi.MemberOperation
		wantReady        bool
		wantOperation    enterpriseApi.MemberOperation
		wantTransfer     bool
	}{
		{name: "detained captain waits for searches", status: "ManualDetention", activeSearches: 1, initialOperation: enterpriseApi.MemberOperationDetaining, wantOperation: enterpriseApi.MemberOperationDraining},
		{name: "detained captain is ready after drain", status: "ManualDetention", initialOperation: enterpriseApi.MemberOperationDetaining, wantReady: true, wantOperation: enterpriseApi.MemberOperationRecycling},
		{name: "transient status waits", initialOperation: enterpriseApi.MemberOperationDetaining, wantOperation: enterpriseApi.MemberOperationDetaining},
		{name: "up captain still transfers", status: "Up", wantOperation: enterpriseApi.MemberOperationDetaining, wantTransfer: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := newTestSHCCR(tc.status, tc.activeSearches, 0)
			cr.Status.Members[0].PodRevision = "revision-1"
			cr.Status.Members[0].CurrentOperation = tc.initialOperation
			cr.Status.Members = append(cr.Status.Members, enterpriseApi.SearchHeadClusterMemberStatus{
				Name: "splunk-test-shc-search-head-1", Status: "Up",
			})
			cr.Status.Captain = cr.Status.Members[0].Name
			cr.Status.CaptainStableSince = time.Now().Unix() - captainStabilizationSeconds - 5
			httpClient := &spltest.MockHTTPClient{}
			mgr := newCaptainTransferCheckpointManager(cr, httpClient)
			if tc.wantTransfer {
				httpClient.AddHandlers(
					spltest.MockHTTPHandler{Method: http.MethodPost, URL: mgr.managementURI(0) + "/services/configs/conf-server/shclustering", Status: http.StatusOK},
					spltest.MockHTTPHandler{Method: http.MethodPost, URL: mgr.managementURI(0) + "/services/shcluster/member/consensus/default/transfer_captaincy", Status: http.StatusOK},
				)
			}

			ready, err := mgr.PrepareRecycle(context.Background(), 0)
			if err != nil || ready != tc.wantReady {
				t.Fatalf("PrepareRecycle = %v, %v; want %v, nil", ready, err, tc.wantReady)
			}
			if got := cr.Status.Members[0].CurrentOperation; got != tc.wantOperation {
				t.Errorf("operation = %q; want %q", got, tc.wantOperation)
			}
			if !tc.wantTransfer {
				httpClient.CheckRequests(t, "PrepareRecycle")
				if got := cr.Status.CaptainTransfer; got != nil {
					t.Errorf("captain transfer checkpoint restarted: %+v", got)
				}
				return
			}
			if got := cr.Status.CaptainTransfer; !hasCaptainTransferFor(got, cr.Status.Members[0].Name) {
				t.Errorf("missing captain transfer checkpoint: %+v", got)
			}
			if cr.Status.DetentionStartTimestamp != 0 || cr.Status.DetainedMemberName != "" || cr.Status.UpgradeStartTimestamp != 0 {
				t.Error("detention or upgrade started before captain transfer completed")
			}
			if cr.Status.Members[0].OperationStartTimestamp == 0 {
				t.Error("operation start timestamp was not set for captain transfer")
			}
			assertCaptainTransferRequests(t, httpClient, mgr.managementURI(1))
		})
	}
}

func TestFinishRecycleCaptainTransferCheckpoint(t *testing.T) {
	const member = "splunk-test-shc-search-head-0"
	for _, tc := range []struct {
		name      string
		status    string
		owner     string
		wantReady bool
		wantError bool
		wantClear bool
	}{
		{name: "completed member clears its checkpoint", status: "Up", owner: member, wantReady: true, wantClear: true},
		{name: "completed member preserves another checkpoint", status: "Up", owner: "splunk-test-shc-search-head-1", wantReady: true},
		{name: "transient status preserves checkpoint", owner: member},
		{name: "release failure preserves checkpoint", status: "ManualDetention", owner: member, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := newTestSHCCR(tc.status, 0, 0)
			before := enterpriseApi.CaptainTransferStatus{MemberName: ptr.To(tc.owner), StartTimestamp: ptr.To(int64(12345))}
			cr.Status.CaptainTransfer = before.DeepCopy()
			httpClient := &spltest.MockHTTPClient{}
			mgr := newCaptainTransferCheckpointManager(cr, httpClient)
			if tc.wantError {
				httpClient.AddHandlers(spltest.MockHTTPHandler{
					Method: http.MethodPost,
					URL:    mgr.managementURI(0) + "/services/shcluster/member/control/control/set_manual_detention?manual_detention=off",
					Status: http.StatusInternalServerError,
				})
			}

			ready, err := mgr.FinishRecycle(context.Background(), 0)
			if (err != nil) != tc.wantError || ready != tc.wantReady {
				t.Fatalf("FinishRecycle = %v, %v; want ready %v, error %v", ready, err, tc.wantReady, tc.wantError)
			}
			want := &before
			if tc.wantClear {
				want = nil
			}
			if got := cr.Status.CaptainTransfer; !reflect.DeepEqual(got, want) {
				t.Errorf("captain transfer checkpoint = %+v; want %+v", got, want)
			}
			httpClient.CheckRequests(t, "FinishRecycle")
		})
	}
}

func TestPrepareScaleDownCaptainTransferCheckpoint(t *testing.T) {
	const member = "splunk-test-shc-search-head-3"
	for _, tc := range []struct {
		name      string
		owner     string
		status    int
		wantError bool
		wantClear bool
	}{
		{name: "successful removal clears its checkpoint", owner: member, status: http.StatusOK, wantClear: true},
		{name: "failed removal preserves its checkpoint", owner: member, status: http.StatusInternalServerError, wantError: true},
		{name: "successful removal preserves another checkpoint", owner: "splunk-test-shc-search-head-2", status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := newTestSHCCR("Up", 0, 0)
			for n := 1; n < 4; n++ {
				cr.Status.Members = append(cr.Status.Members, enterpriseApi.SearchHeadClusterMemberStatus{
					Name: fmt.Sprintf("splunk-test-shc-search-head-%d", n), Status: "Up",
				})
			}
			cr.Status.Members[3].Status = "ManualDetention"
			cr.Status.Captain = cr.Status.Members[0].Name
			cr.Status.CaptainStableSince = time.Now().Unix() - captainStabilizationSeconds - 5
			before := enterpriseApi.CaptainTransferStatus{MemberName: ptr.To(tc.owner), StartTimestamp: ptr.To(int64(12345))}
			cr.Status.CaptainTransfer = before.DeepCopy()
			httpClient := &spltest.MockHTTPClient{}
			mgr := newCaptainTransferCheckpointManager(cr, httpClient)
			httpClient.AddHandlers(spltest.MockHTTPHandler{
				Method: http.MethodPost,
				URL:    mgr.managementURI(3) + "/services/shcluster/member/consensus/default/remove_server?output_mode=json",
				Status: tc.status,
			})

			ready, err := mgr.PrepareScaleDown(context.Background(), 3)
			wantReady := !tc.wantError
			if (err != nil) != tc.wantError || ready != wantReady {
				t.Fatalf("PrepareScaleDown = %v, %v; want ready %v, error %v", ready, err, wantReady, tc.wantError)
			}
			want := &before
			if tc.wantClear {
				want = nil
			}
			if got := cr.Status.CaptainTransfer; !reflect.DeepEqual(got, want) {
				t.Errorf("captain transfer checkpoint = %+v; want %+v", got, want)
			}
			httpClient.CheckRequests(t, "PrepareScaleDown")
		})
	}
}

func TestExpiredCaptainTransferSurvivesPreparation(t *testing.T) {
	originalExecFactory := splutil.GetPodExecClient
	splutil.GetPodExecClient = func(client splcommon.ControllerClient, cr splcommon.MetaObject, pod string) splutil.PodExecClientImpl {
		mock := &spltest.MockPodExecClient{Client: client, Cr: cr, TargetPodName: pod}
		mock.AddMockPodExecReturnContext(context.Background(), "mkdir -p", &spltest.MockPodExecReturnContext{})
		return mock
	}
	t.Cleanup(func() { splutil.GetPodExecClient = originalExecFactory })

	for _, tc := range []struct {
		name          string
		upgradeHTTP   int
		detentionHTTP int
		wantError     bool
	}{
		{name: "upgrade initialization fails", upgradeHTTP: http.StatusServiceUnavailable, wantError: true},
		{name: "detention fails", upgradeHTTP: http.StatusOK, detentionHTTP: http.StatusServiceUnavailable, wantError: true},
		{name: "detention succeeds", upgradeHTTP: http.StatusOK, detentionHTTP: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := newTestSHCCR("Up", 0, 0)
			cr.Status.Members = append(cr.Status.Members, enterpriseApi.SearchHeadClusterMemberStatus{
				Name: "splunk-test-shc-search-head-1", Status: "Up",
			})
			cr.Status.Captain = cr.Status.Members[0].Name
			cr.Status.CaptainStableSince = time.Now().Unix() - captainStabilizationSeconds - 5
			before := enterpriseApi.CaptainTransferStatus{
				MemberName:     ptr.To(cr.Status.Members[0].Name),
				StartTimestamp: ptr.To(time.Now().Unix() - captainTransferTimeoutSeconds - 5),
			}
			cr.Status.CaptainTransfer = before.DeepCopy()
			httpClient := &spltest.MockHTTPClient{}
			mgr := newCaptainTransferCheckpointManager(cr, httpClient)
			upgradeURL := mgr.managementURI(0) + "/services/shcluster/captain/control/control/upgrade-init"
			detentionURL := mgr.managementURI(0) + "/services/shcluster/member/control/control/set_manual_detention?manual_detention=on"
			for attempt := 0; attempt < 2; attempt++ {
				httpClient.AddHandlers(spltest.MockHTTPHandler{Method: http.MethodPost, URL: upgradeURL, Status: tc.upgradeHTTP})
				if tc.detentionHTTP != 0 {
					httpClient.AddHandlers(spltest.MockHTTPHandler{Method: http.MethodPost, URL: detentionURL, Status: tc.detentionHTTP})
				}
			}
			// Unexpected transfer calls are recorded, never sent to a real server.
			httpClient.Handlers["POST "+mgr.managementURI(0)+"/services/configs/conf-server/shclustering"] = spltest.MockHTTPHandler{Status: http.StatusOK}
			httpClient.Handlers["POST "+mgr.managementURI(0)+"/services/shcluster/member/consensus/default/transfer_captaincy"] = spltest.MockHTTPHandler{Status: http.StatusOK}

			for attempt := 0; attempt < 2; attempt++ {
				ready, err := mgr.PrepareRecycle(context.Background(), 0)
				if ready || (err != nil) != tc.wantError {
					t.Errorf("attempt %d = ready %v, error %v; want ready false, error %v", attempt+1, ready, err, tc.wantError)
				}
				t.Logf("attempt %d: checkpoint=%+v requests=%d", attempt+1, cr.Status.CaptainTransfer, len(httpClient.GotRequests))
				if got := cr.Status.CaptainTransfer; !reflect.DeepEqual(got, &before) {
					t.Errorf("attempt %d checkpoint = %+v; want original expired %+v", attempt+1, got, before)
				}
				// Carry returned state into a fresh manager, simulating the next reconcile.
				cr = cr.DeepCopy()
				mgr = newCaptainTransferCheckpointManager(cr, httpClient)
			}
			httpClient.CheckRequests(t, "expired transfer preparation retry")
			for _, req := range httpClient.GotRequests {
				if req.Method != http.MethodPost {
					t.Errorf("request method = %q; want POST", req.Method)
				}
			}
		})
	}
}

func hasCaptainTransferFor(got *enterpriseApi.CaptainTransferStatus, member string) bool {
	return got != nil && got.MemberName != nil && *got.MemberName == member &&
		got.StartTimestamp != nil && *got.StartTimestamp != 0
}
