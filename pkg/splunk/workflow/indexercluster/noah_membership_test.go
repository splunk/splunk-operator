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
			name:     "stale Noah start",
			expected: expected,
			observed: noah.Peer{ID: expected.ID, Data: noah.PeerData{StartTime: startedAt.Unix() - 1}, LastHeartbeat: startedAt.Unix() + 1},
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
