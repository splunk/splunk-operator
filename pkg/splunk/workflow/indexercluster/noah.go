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
	"slices"
	"time"

	"github.com/splunk/splunk-operator/pkg/splunk/client/noah"
)

// ExpectedNoahPeer identifies the Noah peer and Splunk process incarnation
// expected for one IndexerCluster ordinal.
type ExpectedNoahPeer struct {
	ID        string
	StartedAt time.Time
}

// NoahCacheWarmPolicy controls whether membership evaluation reports peers
// that have failed to become ready before the cache-warm deadline.
type NoahCacheWarmPolicy struct {
	Required bool
	Timeout  time.Duration
}

// NoahMembership summarizes the exact relationship between the expected
// IndexerCluster peers and Noah's observed peer records.
type NoahMembership struct {
	AllRegistered  bool
	AllReady       bool
	TimedOutPeerID string
}

// MatchesNoahPeerIncarnation reports whether an observed Noah peer contains
// evidence produced by the expected Splunk process incarnation. A stale peer
// can share its second-resolution start time with a replacement, but it cannot
// produce a heartbeat after the replacement started.
func MatchesNoahPeerIncarnation(expected ExpectedNoahPeer, observed noah.Peer) bool {
	if expected.ID == "" || expected.StartedAt.IsZero() || observed.ID != expected.ID {
		return false
	}

	startedAt := expected.StartedAt.Unix()
	return observed.Data.StartTime >= startedAt && observed.LastHeartbeat > startedAt
}

// EvaluateNoahMembership evaluates registration, readiness, and cache-warm
// timeout from one classification of the observed peers. Foreign and stale
// records are ignored. Each expected peer must have exactly one current record,
// so duplicates and contradictory current records fail closed.
func EvaluateNoahMembership(expected []ExpectedNoahPeer, observed []noah.Peer, policy NoahCacheWarmPolicy, now time.Time) NoahMembership {
	type peerState struct {
		matches    int
		registered int
		ready      int
		timedOut   bool
	}

	expectedByID := make(map[string]ExpectedNoahPeer, len(expected))
	validExpectedSet := len(expected) > 0
	for _, peer := range expected {
		if peer.ID == "" || peer.StartedAt.IsZero() {
			validExpectedSet = false
		}
		if _, duplicate := expectedByID[peer.ID]; duplicate {
			validExpectedSet = false
		}
		expectedByID[peer.ID] = peer
	}

	states := make(map[string]peerState, len(expected))
	for _, peer := range observed {
		expectedPeer, found := expectedByID[peer.ID]
		if !found || !MatchesNoahPeerIncarnation(expectedPeer, peer) {
			continue
		}

		state := states[peer.ID]
		state.matches++
		if noahPeerRegistered(peer.Status) {
			state.registered++
		}
		if peer.Status == noah.PeerStatusUp {
			state.ready++
		} else if policy.Required && policy.Timeout > 0 && !now.Before(time.Unix(peer.Data.StartTime, 0).Add(policy.Timeout)) {
			state.timedOut = true
		}
		states[peer.ID] = state
	}

	result := NoahMembership{AllRegistered: validExpectedSet, AllReady: validExpectedSet}
	for _, expectedPeer := range expected {
		state := states[expectedPeer.ID]
		if state.matches != 1 || state.registered != 1 {
			result.AllRegistered = false
		}
		if state.matches != 1 || state.ready != 1 {
			result.AllReady = false
		}

		timedOut := state.timedOut
		if state.matches == 0 && policy.Required && policy.Timeout > 0 {
			timedOut = !now.Before(expectedPeer.StartedAt.Add(policy.Timeout))
		}
		if timedOut && (result.TimedOutPeerID == "" || expectedPeer.ID < result.TimedOutPeerID) {
			result.TimedOutPeerID = expectedPeer.ID
		}
	}
	return result
}

func noahPeerRegistered(status noah.PeerStatus) bool {
	switch status {
	case noah.PeerStatusStarted, noah.PeerStatusWarming, noah.PeerStatusWarmed, noah.PeerStatusUp:
		return true
	default:
		return false
	}
}

// NoahBucketMapConfirmsScaleDown reports whether Noah's latest bucket map is
// active, includes every remaining indexer peer, and excludes the removed peer.
func NoahBucketMapConfirmsScaleDown(bucketMap *noah.BucketMap, remainingPeerIDs []string, removedPeerID string) bool {
	if bucketMap == nil || bucketMap.ID <= 0 || bucketMap.Status != noah.BucketMapStatusActive || bucketMap.PeerIDs == nil || removedPeerID == "" {
		return false
	}

	if slices.Contains(bucketMap.PeerIDs, removedPeerID) {
		return false
	}

	for _, peerID := range remainingPeerIDs {
		if peerID == "" || !slices.Contains(bucketMap.PeerIDs, peerID) {
			return false
		}
	}

	return true
}
