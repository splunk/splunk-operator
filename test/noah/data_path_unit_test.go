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

package noah

import "testing"

func TestMarkerPeer(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		expected  string
		wantError bool
	}{
		{
			name:     "extracts peer from marker event",
			raw:      "marker=noahc3data peer=splunk-c3-indexer-1",
			expected: "splunk-c3-indexer-1",
		},
		{
			name:     "accepts surrounding fields and whitespace",
			raw:      "source=test\npeer=splunk-c3-indexer-0 marker=noahc3data",
			expected: "splunk-c3-indexer-0",
		},
		{
			name:      "rejects missing peer",
			raw:       "marker=noahc3data",
			wantError: true,
		},
		{
			name:      "rejects empty peer",
			raw:       "marker=noahc3data peer=",
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			peer, err := markerPeer(test.raw)
			if test.wantError {
				if err == nil {
					t.Fatalf("markerPeer(%q) unexpectedly returned %q", test.raw, peer)
				}
				return
			}
			if err != nil {
				t.Fatalf("markerPeer(%q) returned an error: %v", test.raw, err)
			}
			if peer != test.expected {
				t.Errorf("markerPeer(%q) = %q, want %q", test.raw, peer, test.expected)
			}
		})
	}
}
