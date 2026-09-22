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
	"maps"
	"slices"
	"time"

	"github.com/splunk/splunk-operator/pkg/splunk/client/noah"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
)

// ExpectedNoahPeer identifies the Noah peer and Splunk process incarnation
// expected for one IndexerCluster ordinal.
type ExpectedNoahPeer struct {
	ID        string
	PodName   string
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
	Peers             []NoahPeerMembership
	UnexpectedPeerIDs []string
	AllRegistered     bool
	AllReady          bool
	TimedOutPeerID    string
}

// NoahPeerClassification describes how Noah's records relate to one expected
// IndexerCluster peer.
type NoahPeerClassification string

const (
	NoahPeerCurrent       NoahPeerClassification = "Current"
	NoahPeerMissing       NoahPeerClassification = "Missing"
	NoahPeerStale         NoahPeerClassification = "Stale"
	NoahPeerDuplicate     NoahPeerClassification = "Duplicate"
	NoahPeerContradictory NoahPeerClassification = "Contradictory"
	NoahPeerInvalid       NoahPeerClassification = "Invalid"
)

// NoahPeerMembership classifies Noah's observations for one expected peer.
type NoahPeerMembership struct {
	Expected       ExpectedNoahPeer
	Classification NoahPeerClassification
	Status         noah.PeerStatus
	Registered     bool
	Ready          bool
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
	expectedCounts := make(map[string]int, len(expected))
	for _, peer := range expected {
		expectedCounts[peer.ID]++
	}

	observedByID := make(map[string][]noah.Peer, len(observed))
	unexpectedIDs := make(map[string]struct{})
	for _, peer := range observed {
		observedByID[peer.ID] = append(observedByID[peer.ID], peer)
		if expectedCounts[peer.ID] == 0 {
			unexpectedIDs[peer.ID] = struct{}{}
		}
	}

	result := NoahMembership{
		Peers:             make([]NoahPeerMembership, 0, len(expected)),
		UnexpectedPeerIDs: slices.Sorted(maps.Keys(unexpectedIDs)),
		AllRegistered:     true,
		AllReady:          true,
	}

	for _, expectedPeer := range expected {
		peerMembership := NoahPeerMembership{Expected: expectedPeer}
		if expectedPeer.ID == "" || expectedPeer.StartedAt.IsZero() || expectedCounts[expectedPeer.ID] != 1 {
			peerMembership.Classification = NoahPeerInvalid
			result.AllRegistered = false
			result.AllReady = false
			result.Peers = append(result.Peers, peerMembership)
			continue
		}

		matches := make([]noah.Peer, 0, len(observedByID[expectedPeer.ID]))
		for _, peer := range observedByID[expectedPeer.ID] {
			if MatchesNoahPeerIncarnation(expectedPeer, peer) {
				matches = append(matches, peer)
			}
		}

		switch len(matches) {
		case 0:
			peerMembership.Classification = NoahPeerMissing
			if len(observedByID[expectedPeer.ID]) > 0 {
				peerMembership.Classification = NoahPeerStale
			}
		case 1:
			peerMembership.Classification = NoahPeerCurrent
			peerMembership.Status = matches[0].Status
			peerMembership.Registered = noahPeerRegistered(matches[0].Status)
			peerMembership.Ready = matches[0].Status == noah.PeerStatusUp
		default:
			peerMembership.Classification = NoahPeerDuplicate
			for _, peer := range matches[1:] {
				if peer.Status != matches[0].Status {
					peerMembership.Classification = NoahPeerContradictory
					break
				}
			}
		}

		if !peerMembership.Registered {
			result.AllRegistered = false
		}
		if !peerMembership.Ready {
			result.AllReady = false
		}

		timedOut := false
		if policy.Required && policy.Timeout > 0 {
			if len(matches) == 0 {
				timedOut = !now.Before(expectedPeer.StartedAt.Add(policy.Timeout))
			} else {
				for _, peer := range matches {
					if peer.Status != noah.PeerStatusUp && !now.Before(time.Unix(peer.Data.StartTime, 0).Add(policy.Timeout)) {
						timedOut = true
						break
					}
				}
			}
		}
		if timedOut && (result.TimedOutPeerID == "" || expectedPeer.ID < result.TimedOutPeerID) {
			result.TimedOutPeerID = expectedPeer.ID
		}

		result.Peers = append(result.Peers, peerMembership)
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

// PlanNoahScaleOut returns the next safe replica target. Scale-out advances by
// at most one ordinal after all applied peers satisfy the selected gate. Final
// convergence always requires every applied peer to be ready in Noah.
func PlanNoahScaleOut(membership NoahMembership, appliedReplicas, requestedReplicas int32, requireReady bool) common.ScaleOutPlan {
	registered := membership.AllRegistered && membership.TimedOutPeerID == ""
	ready := registered && membership.AllReady
	plan := common.ScaleOutPlan{
		Complete:       ready && appliedReplicas == requestedReplicas,
		TargetReplicas: appliedReplicas,
	}

	canAdvance := registered
	if requireReady {
		canAdvance = ready
	}

	if canAdvance && appliedReplicas < requestedReplicas {
		plan.TargetReplicas++
	}

	return plan
}
