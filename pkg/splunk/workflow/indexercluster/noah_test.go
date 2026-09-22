// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package indexercluster

import (
	"testing"
	"time"

	"github.com/splunk/splunk-operator/pkg/splunk/client/noah"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/assert"
)

func TestMatchesNoahPeerIncarnation(t *testing.T) {
	startedAt := time.Unix(1_700_000_000, 0)
	expected := ExpectedNoahPeer{ID: "peer-0", StartedAt: startedAt}
	current := noah.Peer{
		ID:            expected.ID,
		Data:          noah.PeerData{StartTime: startedAt.Unix()},
		LastHeartbeat: startedAt.Unix() + 1,
	}

	tests := []struct {
		name     string
		expected ExpectedNoahPeer
		observed noah.Peer
		want     bool
	}{
		{name: "current incarnation", expected: expected, observed: current, want: true},
		{
			name:     "Noah start after container start",
			expected: expected,
			observed: noah.Peer{ID: expected.ID, Data: noah.PeerData{StartTime: startedAt.Unix() + 1}, LastHeartbeat: startedAt.Unix() + 2},
			want:     true,
		},
		{name: "empty expected ID", expected: ExpectedNoahPeer{StartedAt: startedAt}, observed: current},
		{name: "zero expected start", expected: ExpectedNoahPeer{ID: expected.ID}, observed: current},
		{
			name:     "different peer ID",
			expected: expected,
			observed: noah.Peer{ID: "peer-1", Data: current.Data, LastHeartbeat: current.LastHeartbeat},
		},
		{
			name:     "same-name workload recreation rejects previous process start",
			expected: expected,
			observed: noah.Peer{ID: expected.ID, Data: noah.PeerData{StartTime: startedAt.Unix() - 1}, LastHeartbeat: startedAt.Unix() + 1},
		},
		{
			name:     "missing Noah start",
			expected: expected,
			observed: noah.Peer{ID: expected.ID, LastHeartbeat: startedAt.Unix() + 1},
		},
		{
			name:     "missing heartbeat",
			expected: expected,
			observed: noah.Peer{ID: expected.ID, Data: noah.PeerData{StartTime: startedAt.Unix()}},
		},
		{
			name:     "same-second stale heartbeat",
			expected: expected,
			observed: noah.Peer{ID: expected.ID, Data: noah.PeerData{StartTime: startedAt.Unix()}, LastHeartbeat: startedAt.Unix()},
		},
		{
			name:     "heartbeat before container start",
			expected: expected,
			observed: noah.Peer{ID: expected.ID, Data: noah.PeerData{StartTime: startedAt.Unix()}, LastHeartbeat: startedAt.Unix() - 1},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, MatchesNoahPeerIncarnation(test.expected, test.observed))
		})
	}
}

func TestEvaluateNoahMembership(t *testing.T) {
	startedAt := time.Unix(1_700_000_000, 0)
	expected := []ExpectedNoahPeer{
		{ID: "peer-0", StartedAt: startedAt},
		{ID: "peer-1", StartedAt: startedAt},
	}
	peer := func(id string, status noah.PeerStatus) noah.Peer {
		return noah.Peer{
			ID:            id,
			Status:        status,
			Data:          noah.PeerData{StartTime: startedAt.Unix()},
			LastHeartbeat: startedAt.Unix() + 1,
		}
	}
	up := []noah.Peer{peer("peer-0", noah.PeerStatusUp), peer("peer-1", noah.PeerStatusUp)}

	tests := []struct {
		name     string
		expected []ExpectedNoahPeer
		observed []noah.Peer
		want     NoahMembership
	}{
		{
			name:     "exact expected set is registered and ready",
			expected: expected,
			observed: up,
			want:     NoahMembership{AllRegistered: true, AllReady: true},
		},
		{
			name:     "missing expected peer fails closed",
			expected: expected,
			observed: up[:1],
		},
		{
			name:     "non-ready expected peer is registered",
			expected: expected,
			observed: []noah.Peer{up[0], peer("peer-1", noah.PeerStatusWarming)},
			want:     NoahMembership{AllRegistered: true},
		},
		{
			name:     "down expected peer is neither registered nor ready",
			expected: expected,
			observed: []noah.Peer{up[0], peer("peer-1", noah.PeerStatusDown)},
		},
		{
			name:     "foreign peer is ignored",
			expected: expected,
			observed: append(up, peer("foreign", noah.PeerStatusUp)),
			want:     NoahMembership{AllRegistered: true, AllReady: true},
		},
		{
			name:     "wrong advertised identity does not satisfy membership",
			expected: expected,
			observed: []noah.Peer{up[0], peer("indexer-1", noah.PeerStatusUp)},
		},
		{
			name:     "stale peer is ignored",
			expected: expected,
			observed: []noah.Peer{up[0], {ID: "peer-1", Status: noah.PeerStatusUp, Data: noah.PeerData{StartTime: startedAt.Unix() - 1}, LastHeartbeat: startedAt.Unix()}},
		},
		{
			name:     "historical peer does not conflict with current peer",
			expected: expected,
			observed: append(up, noah.Peer{ID: "peer-1", Status: noah.PeerStatusDown, Data: noah.PeerData{StartTime: startedAt.Unix() - 1}, LastHeartbeat: startedAt.Unix()}),
			want:     NoahMembership{AllRegistered: true, AllReady: true},
		},
		{
			name:     "duplicate current peer fails closed",
			expected: expected,
			observed: append(up, peer("peer-1", noah.PeerStatusUp)),
		},
		{
			name:     "contradictory current peers fail closed",
			expected: expected,
			observed: append(up, peer("peer-1", noah.PeerStatusDown)),
		},
		{
			name:     "empty expected set is converged",
			expected: nil,
			observed: up,
			want:     NoahMembership{AllRegistered: true, AllReady: true},
		},
		{
			name: "duplicate expected identity is invalid",
			expected: []ExpectedNoahPeer{
				{ID: "peer-0", StartedAt: startedAt},
				{ID: "peer-0", StartedAt: startedAt},
			},
			observed: up[:1],
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := EvaluateNoahMembership(test.expected, test.observed, NoahCacheWarmPolicy{}, startedAt)
			assert.Equal(t, test.want.AllRegistered, got.AllRegistered)
			assert.Equal(t, test.want.AllReady, got.AllReady)
			assert.Equal(t, test.want.TimedOutPeerID, got.TimedOutPeerID)
		})
	}
}

func TestEvaluateNoahMembershipClassifiesExpectedPeers(t *testing.T) {
	startedAt := time.Unix(1_700_000_000, 0)
	expected := []ExpectedNoahPeer{
		{ID: "current", PodName: "indexer-0", StartedAt: startedAt},
		{ID: "missing", PodName: "indexer-1", StartedAt: startedAt},
		{ID: "stale", PodName: "indexer-2", StartedAt: startedAt},
		{ID: "duplicate", PodName: "indexer-3", StartedAt: startedAt},
		{ID: "contradictory", PodName: "indexer-4", StartedAt: startedAt},
		{ID: "invalid", PodName: "indexer-5"},
	}
	peer := func(id string, status noah.PeerStatus) noah.Peer {
		return noah.Peer{
			ID:            id,
			Status:        status,
			Data:          noah.PeerData{StartTime: startedAt.Unix()},
			LastHeartbeat: startedAt.Unix() + 1,
		}
	}
	observed := []noah.Peer{
		peer("current", noah.PeerStatusUp),
		{ID: "stale", Status: noah.PeerStatusUp, Data: noah.PeerData{StartTime: startedAt.Unix() - 1}, LastHeartbeat: startedAt.Unix()},
		peer("duplicate", noah.PeerStatusUp),
		peer("duplicate", noah.PeerStatusUp),
		peer("contradictory", noah.PeerStatusUp),
		peer("contradictory", noah.PeerStatusDown),
		peer("foreign-b", noah.PeerStatusUp),
		peer("foreign-a", noah.PeerStatusUp),
		peer("foreign-b", noah.PeerStatusDown),
	}

	got := EvaluateNoahMembership(expected, observed, NoahCacheWarmPolicy{}, startedAt)
	classifications := make([]NoahPeerClassification, 0, len(got.Peers))
	for _, peer := range got.Peers {
		classifications = append(classifications, peer.Classification)
	}

	assert.Equal(t, []NoahPeerClassification{
		NoahPeerCurrent,
		NoahPeerMissing,
		NoahPeerStale,
		NoahPeerDuplicate,
		NoahPeerContradictory,
		NoahPeerInvalid,
	}, classifications)
	if assert.NotEmpty(t, got.Peers) {
		assert.Equal(t, noah.PeerStatusUp, got.Peers[0].Status)
		assert.True(t, got.Peers[0].Registered)
		assert.True(t, got.Peers[0].Ready)
	}
	assert.Equal(t, []string{"foreign-a", "foreign-b"}, got.UnexpectedPeerIDs)
	assert.False(t, got.AllRegistered)
	assert.False(t, got.AllReady)
}

func TestEvaluateNoahMembershipAcceptsExplicitZero(t *testing.T) {
	got := EvaluateNoahMembership(nil, []noah.Peer{{ID: "unrelated"}}, NoahCacheWarmPolicy{}, time.Now())

	assert.Empty(t, got.Peers)
	assert.Equal(t, []string{"unrelated"}, got.UnexpectedPeerIDs)
	assert.True(t, got.AllRegistered)
	assert.True(t, got.AllReady)
}

func TestEvaluateNoahMembershipRegistrationStatuses(t *testing.T) {
	startedAt := time.Unix(1_700_000_000, 0)
	expected := []ExpectedNoahPeer{{ID: "peer-0", StartedAt: startedAt}}

	tests := []struct {
		status     noah.PeerStatus
		registered bool
		ready      bool
	}{
		{status: noah.PeerStatusStarted, registered: true},
		{status: noah.PeerStatusWarming, registered: true},
		{status: noah.PeerStatusWarmed, registered: true},
		{status: noah.PeerStatusUp, registered: true, ready: true},
		{status: noah.PeerStatusDown},
		{status: noah.PeerStatusDecommissionReady},
		{status: noah.PeerStatusDecommissioning},
		{status: noah.PeerStatusDecommissioned},
		{status: noah.PeerStatusUnknown},
	}

	for _, test := range tests {
		t.Run(string(test.status), func(t *testing.T) {
			observed := []noah.Peer{{
				ID:            "peer-0",
				Status:        test.status,
				Data:          noah.PeerData{StartTime: startedAt.Unix()},
				LastHeartbeat: startedAt.Unix() + 1,
			}}
			got := EvaluateNoahMembership(expected, observed, NoahCacheWarmPolicy{}, startedAt)
			assert.Equal(t, test.registered, got.AllRegistered)
			assert.Equal(t, test.ready, got.AllReady)
		})
	}
}

func TestEvaluateNoahMembershipCacheWarmTimeout(t *testing.T) {
	startedAt := time.Unix(1_700_000_000, 0)
	now := startedAt.Add(time.Minute)
	policy := NoahCacheWarmPolicy{Required: true, Timeout: time.Minute}
	expected := []ExpectedNoahPeer{{ID: "peer-b", StartedAt: startedAt}, {ID: "peer-a", StartedAt: startedAt}}
	peer := func(id string, status noah.PeerStatus, start time.Time) noah.Peer {
		return noah.Peer{ID: id, Status: status, Data: noah.PeerData{StartTime: start.Unix()}, LastHeartbeat: start.Unix() + 1}
	}

	tests := []struct {
		name     string
		observed []noah.Peer
		policy   NoahCacheWarmPolicy
		now      time.Time
		want     string
	}{
		{name: "missing peers time out deterministically", policy: policy, now: now, want: "peer-a"},
		{name: "warming peers time out deterministically", observed: []noah.Peer{peer("peer-b", noah.PeerStatusWarming, startedAt), peer("peer-a", noah.PeerStatusWarming, startedAt)}, policy: policy, now: now, want: "peer-a"},
		{name: "started peer times out", observed: []noah.Peer{peer("peer-b", noah.PeerStatusUp, startedAt), peer("peer-a", noah.PeerStatusStarted, startedAt)}, policy: policy, now: now, want: "peer-a"},
		{name: "warmed peer must still become up", observed: []noah.Peer{peer("peer-b", noah.PeerStatusUp, startedAt), peer("peer-a", noah.PeerStatusWarmed, startedAt)}, policy: policy, now: now, want: "peer-a"},
		{name: "down peer times out", observed: []noah.Peer{peer("peer-b", noah.PeerStatusUp, startedAt), peer("peer-a", noah.PeerStatusDown, startedAt)}, policy: policy, now: now, want: "peer-a"},
		{name: "missing peers remain within timeout", policy: policy, now: now.Add(-time.Second)},
		{name: "warming peers remain within timeout", observed: []noah.Peer{peer("peer-b", noah.PeerStatusWarming, startedAt), peer("peer-a", noah.PeerStatusWarming, startedAt)}, policy: policy, now: now.Add(-time.Second)},
		{name: "up peers do not time out", observed: []noah.Peer{peer("peer-b", noah.PeerStatusUp, startedAt), peer("peer-a", noah.PeerStatusUp, startedAt)}, policy: policy, now: now},
		{name: "timeout policy can be disabled", policy: NoahCacheWarmPolicy{Timeout: time.Minute}, now: now},
		{name: "zero timeout is disabled", policy: NoahCacheWarmPolicy{Required: true}, now: now},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, EvaluateNoahMembership(expected, test.observed, test.policy, test.now).TimedOutPeerID)
		})
	}
}

func TestNoahBucketMapConfirmsScaleDown(t *testing.T) {
	valid := &noah.BucketMap{
		ID:      7,
		Status:  noah.BucketMapStatusActive,
		PeerIDs: []string{"peer-0", "peer-1"},
	}

	tests := []struct {
		name      string
		bucketMap *noah.BucketMap
		remaining []string
		removed   string
		want      bool
	}{
		{name: "remaining peers present and removed peer absent", bucketMap: valid, remaining: []string{"peer-0", "peer-1"}, removed: "peer-2", want: true},
		{name: "foreign peers are allowed", bucketMap: &noah.BucketMap{ID: 7, Status: noah.BucketMapStatusActive, PeerIDs: []string{"peer-0", "foreign"}}, remaining: []string{"peer-0"}, removed: "peer-1", want: true},
		{name: "explicit empty remaining set is complete", bucketMap: &noah.BucketMap{ID: 7, Status: noah.BucketMapStatusActive, PeerIDs: []string{}}, removed: "peer-0", want: true},
		{name: "missing bucket map", remaining: []string{"peer-0"}, removed: "peer-2"},
		{name: "zero map ID", bucketMap: &noah.BucketMap{Status: noah.BucketMapStatusActive, PeerIDs: []string{"peer-0"}}, remaining: []string{"peer-0"}, removed: "peer-2"},
		{name: "negative map ID", bucketMap: &noah.BucketMap{ID: -1, Status: noah.BucketMapStatusActive, PeerIDs: []string{"peer-0"}}, remaining: []string{"peer-0"}, removed: "peer-2"},
		{name: "unknown status", bucketMap: &noah.BucketMap{ID: 7, PeerIDs: []string{"peer-0"}}, remaining: []string{"peer-0"}, removed: "peer-2"},
		{name: "omitted peer list", bucketMap: &noah.BucketMap{ID: 7, Status: noah.BucketMapStatusActive}, remaining: []string{"peer-0"}, removed: "peer-2"},
		{name: "removed peer remains", bucketMap: &noah.BucketMap{ID: 7, Status: noah.BucketMapStatusActive, PeerIDs: []string{"peer-0", "peer-2"}}, remaining: []string{"peer-0"}, removed: "peer-2"},
		{name: "remaining peer missing", bucketMap: valid, remaining: []string{"peer-0", "peer-3"}, removed: "peer-2"},
		{name: "empty remaining peer ID", bucketMap: valid, remaining: []string{"peer-0", ""}, removed: "peer-2"},
		{name: "empty removed peer ID", bucketMap: valid, remaining: []string{"peer-0"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, NoahBucketMapConfirmsScaleDown(test.bucketMap, test.remaining, test.removed))
		})
	}
}

func TestPlanNoahScaleOut(t *testing.T) {
	tests := []struct {
		name              string
		membership        NoahMembership
		appliedReplicas   int32
		requestedReplicas int32
		requireReady      bool
		want              common.ScaleOutPlan
	}{
		{
			name:              "unknown membership blocks progress",
			appliedReplicas:   1,
			requestedReplicas: 3,
			want:              common.ScaleOutPlan{TargetReplicas: 1},
		},
		{
			name:              "registration advances one ordinal when readiness is optional",
			membership:        NoahMembership{AllRegistered: true},
			appliedReplicas:   1,
			requestedReplicas: 3,
			want:              common.ScaleOutPlan{TargetReplicas: 2},
		},
		{
			name:              "registration alone cannot advance when readiness is required",
			membership:        NoahMembership{AllRegistered: true},
			appliedReplicas:   1,
			requestedReplicas: 3,
			requireReady:      true,
			want:              common.ScaleOutPlan{TargetReplicas: 1},
		},
		{
			name:              "contradictory readiness aggregate fails closed",
			membership:        NoahMembership{AllReady: true},
			appliedReplicas:   1,
			requestedReplicas: 3,
			requireReady:      true,
			want:              common.ScaleOutPlan{TargetReplicas: 1},
		},
		{
			name:              "cache warm timeout fails closed",
			membership:        NoahMembership{AllRegistered: true, AllReady: true, TimedOutPeerID: "peer-0"},
			appliedReplicas:   1,
			requestedReplicas: 3,
			want:              common.ScaleOutPlan{TargetReplicas: 1},
		},
		{
			name:              "readiness advances only one ordinal",
			membership:        NoahMembership{AllRegistered: true, AllReady: true},
			appliedReplicas:   1,
			requestedReplicas: 5,
			requireReady:      true,
			want:              common.ScaleOutPlan{TargetReplicas: 2},
		},
		{
			name:              "requested replicas and readiness complete scale-out",
			membership:        NoahMembership{AllRegistered: true, AllReady: true},
			appliedReplicas:   3,
			requestedReplicas: 3,
			requireReady:      true,
			want:              common.ScaleOutPlan{Complete: true, TargetReplicas: 3},
		},
		{
			name:              "registered final peer is not complete until ready",
			membership:        NoahMembership{AllRegistered: true},
			appliedReplicas:   3,
			requestedReplicas: 3,
			want:              common.ScaleOutPlan{TargetReplicas: 3},
		},
		{
			name:              "lower requested replicas cannot trigger scale-out",
			membership:        NoahMembership{AllRegistered: true, AllReady: true},
			appliedReplicas:   3,
			requestedReplicas: 2,
			want:              common.ScaleOutPlan{TargetReplicas: 3},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, PlanNoahScaleOut(
				test.membership,
				test.appliedReplicas,
				test.requestedReplicas,
				test.requireReady,
			))
		})
	}
}
