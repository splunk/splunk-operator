// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	appsv1 "k8s.io/api/apps/v1"
)

func TestLifecycleAdapterPersistsStagesBeforeActions(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)

	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	oldNow := searchHeadClusterLifecycleNow
	oldGetMembers := getSearchHeadCaptainMembers
	oldRequestDetention := requestSearchHeadDetention
	oldTransferCaptain := transferSearchHeadCaptain
	t.Cleanup(func() {
		searchHeadClusterLifecycleNow = oldNow
		getSearchHeadCaptainMembers = oldGetMembers
		requestSearchHeadDetention = oldRequestDetention
		transferSearchHeadCaptain = oldTransferCaptain
	})
	searchHeadClusterLifecycleNow = func() time.Time {
		now = now.Add(time.Second)
		return now
	}

	cr := &enterpriseApi.SearchHeadCluster{}
	cr.Name = "example"
	cr.Status.Initialized = true
	cr.Status.MinPeersJoined = true
	cr.Status.CaptainReady = true
	cr.Status.Captain = "splunk-example-search-head-2"
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{
			Name:       "splunk-example-search-head-0",
			Status:     "Up",
			Registered: true,
		},
		{
			Name:       "splunk-example-search-head-1",
			Status:     "Up",
			Registered: true,
		},
		{
			Name:       "splunk-example-search-head-2",
			Status:     "Up",
			Registered: true,
		},
	}
	mgr := &searchHeadClusterPodManager{
		cr: cr,
		statefulSet: &appsv1.StatefulSet{
			Status: appsv1.StatefulSetStatus{UpdateRevision: "revision-2"},
		},
	}

	captainMembers := map[string]splclient.SearchHeadCaptainMemberInfo{
		"splunk-example-search-head-0": {
			Label:         "splunk-example-search-head-0",
			Status:        "Up",
			ManagementURI: "https://splunk-example-search-head-0:8089",
		},
		"splunk-example-search-head-1": {
			Label:            "splunk-example-search-head-1",
			Status:           "Up",
			ManagementURI:    "https://splunk-example-search-head-1:8089",
			PreferredCaptain: true,
		},
		"splunk-example-search-head-2": {
			Label:         "splunk-example-search-head-2",
			Status:        "Up",
			Captain:       true,
			ManagementURI: "https://splunk-example-search-head-2:8089",
		},
	}
	getSearchHeadCaptainMembers = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
		return captainMembers, nil
	}

	detentionCalls := 0
	requestSearchHeadDetention = func(context.Context, *searchHeadClusterPodManager, int32) error {
		detentionCalls++
		return nil
	}
	transferCalls := 0
	transferTarget := ""
	transferSearchHeadCaptain = func(
		_ context.Context,
		_ *searchHeadClusterPodManager,
		_ int32,
		managementURI string,
	) error {
		transferCalls++
		transferTarget = managementURI
		return nil
	}

	// Reconcile 1 persists operation identity; no action is allowed.
	ready, err := mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if detentionCalls != 0 || transferCalls != 0 {
		t.Fatal("adapter executed an action before operation identity was persisted")
	}

	// Reconcile 2 persists DetainingTarget; detention is still not called.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if cr.Status.LifecycleOperation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget {
		t.Fatalf("stage = %q, want DetainingTarget", cr.Status.LifecycleOperation.Stage)
	}
	if detentionCalls != 0 {
		t.Fatal("detention executed in the same reconcile as its stage transition")
	}

	// Reconcile 3 observes the persisted stage and may request detention.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if detentionCalls != 1 {
		t.Fatalf("detention calls = %d, want 1", detentionCalls)
	}

	// Once detained and drained, the next reconcile persists
	// TransferringCaptain but does not yet call the transfer endpoint.
	cr.Status.Members[2].Status = "ManualDetention"
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if cr.Status.LifecycleOperation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain {
		t.Fatalf("stage = %q, want TransferringCaptain", cr.Status.LifecycleOperation.Stage)
	}
	if transferCalls != 0 {
		t.Fatal("captain transfer executed in the same reconcile as its stage transition")
	}

	// The persisted transfer stage authorizes one transfer request.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if transferCalls != 1 {
		t.Fatalf("transfer calls = %d, want 1", transferCalls)
	}
	if transferTarget != "https://splunk-example-search-head-1:8089" {
		t.Fatalf("transfer target = %q, want preferred captain candidate", transferTarget)
	}
	if cr.Status.LifecycleOperation.CaptainTransferRequestedAt == nil {
		t.Fatal("successful captain transfer submission was not recorded")
	}

	// Restart/resume with the submitted operation only observes; it does not
	// submit the non-idempotent transfer request again.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if transferCalls != 1 {
		t.Fatalf("transfer calls after resume = %d, want 1", transferCalls)
	}

	// A fresh observation of a different ready captain persists replacement
	// authorization, but cannot authorize deletion in the same reconcile.
	cr.Status.Captain = "splunk-example-search-head-0"
	captainMembers["splunk-example-search-head-2"] = splclient.SearchHeadCaptainMemberInfo{
		Label:         "splunk-example-search-head-2",
		Status:        "ManualDetention",
		ManagementURI: "https://splunk-example-search-head-2:8089",
	}
	captainMembers["splunk-example-search-head-0"] = splclient.SearchHeadCaptainMemberInfo{
		Label:         "splunk-example-search-head-0",
		Status:        "Up",
		Captain:       true,
		ManagementURI: "https://splunk-example-search-head-0:8089",
	}
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if cr.Status.LifecycleOperation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement {
		t.Fatalf("stage = %q, want AuthorizingReplacement", cr.Status.LifecycleOperation.Stage)
	}

	// Only a later reconcile observing the durable authorization returns true
	// to the existing Pod manager.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, true)
}

func TestLifecycleObservationRejectsCaptainDisagreement(t *testing.T) {
	cr := &enterpriseApi.SearchHeadCluster{}
	cr.Name = "example"
	cr.Status.Initialized = true
	cr.Status.MinPeersJoined = true
	cr.Status.CaptainReady = true
	cr.Status.Captain = "splunk-example-search-head-0"
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: "splunk-example-search-head-0", Status: "Up", Registered: true},
		{Name: "splunk-example-search-head-1", Status: "Up", Registered: true},
	}
	mgr := &searchHeadClusterPodManager{cr: cr}

	oldGetMembers := getSearchHeadCaptainMembers
	t.Cleanup(func() { getSearchHeadCaptainMembers = oldGetMembers })
	getSearchHeadCaptainMembers = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
		return map[string]splclient.SearchHeadCaptainMemberInfo{
			"splunk-example-search-head-0": {
				Label:  "splunk-example-search-head-0",
				Status: "Up",
			},
			"splunk-example-search-head-1": {
				Label:   "splunk-example-search-head-1",
				Status:  "Up",
				Captain: true,
			},
		}, nil
	}

	observation := mgr.observeLifecycleReplacement(context.Background(), 1, time.Now())
	if !observation.Available || !observation.Fresh {
		t.Fatal("expected a fresh observation")
	}
	if !observation.ConflictingCaptain {
		t.Fatal("expected disagreement between captain info and captain member view")
	}
}

func assertLifecycleAdapterResult(t *testing.T, got bool, err error, want bool) {
	t.Helper()
	if err != nil {
		t.Fatalf("prepare lifecycle replacement: %v", err)
	}
	if got != want {
		t.Fatalf("ready = %t, want %t", got, want)
	}
}
